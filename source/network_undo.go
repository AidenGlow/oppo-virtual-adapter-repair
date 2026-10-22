package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

const networkReceiptSuffix = ".oppo-repair.json"

type networkReceipt struct {
	Format       int    `json:"format"`
	Adapter      string `json:"adapter"`
	BackupSHA256 string `json:"backup_sha256"`
}

func contentSHA256(data []byte) string { sum := sha256.Sum256(data); return hex.EncodeToString(sum[:]) }

// 记录仅含版本、网卡名和原备份摘要，不复制节点、规则或密码。
func writeNetworkReceipt(backup, adapter string, original []byte) error {
	receipt := networkReceipt{1, adapter, contentSHA256(original)}
	data, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil {
		return err
	}
	path := backup + networkReceiptSuffix
	file, err := createReceiptFile(backup, path)
	if err != nil {
		return err
	}
	success := false
	defer func() {
		file.Close()
		if !success {
			os.Remove(path)
		}
	}()
	if _, err = file.Write(append(data, '\n')); err != nil {
		return err
	}
	if err = file.Sync(); err != nil {
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	success = true

	return nil
}

type networkScalar struct {
	Present    bool
	Tag, Value string
}

func scalarState(value *yaml.Node) networkScalar {
	if value == nil {
		return networkScalar{}
	}
	return networkScalar{true, value.Tag, value.Value}
}
func sameNetworkScalar(a, b networkScalar) bool {
	if a.Present != b.Present || a.Tag != b.Tag {
		return false
	}
	return a.Value == b.Value || a.Tag == "!!bool" && strings.EqualFold(a.Value, b.Value)
}
func undoField(mapping *yaml.Node, name string) {
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == name {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			return
		}
	}
}
func undoScalarText(s networkScalar) (string, error) {
	switch s.Tag {
	case "!!str":
		return strconv.Quote(s.Value), nil
	case "!!bool", "!!null":
		return s.Value, nil
	}
	return "", errors.New("原网卡字段不是受支持的字符串、布尔值或空值")
}
func deleteYAMLScalarLine(body []byte, lines []yamlSourceLine, key, value *yaml.Node) (yamlTextEdit, error) {
	line, text, _, err := yamlTargetLine(body, lines, key)
	if err != nil {
		return yamlTextEdit{}, err
	}
	if _, err = yamlScalarEdit(body, lines, key, value, "null"); err != nil {
		return yamlTextEdit{}, err
	}
	comment := yamlCommentStart(text, yamlColon(text)+1)
	// 从值起始处解析注释，避免把引号里的 # 当成注释。
	start := yamlColon(text) + 1
	for start < len(text) && (text[start] == ' ' || text[start] == '\t') {
		start++
	}
	comment = yamlCommentStart(text, start)
	replacement := ""
	if comment < len(text) {
		indent := text[:len(text)-len(strings.TrimLeft(text, " "))]
		replacement = indent + text[comment:] + string(body[line.contentEnd:line.end])
	}
	return yamlTextEdit{line.start, line.end, replacement}, nil
}
func applyUndoEdits(raw []byte, edits []yamlTextEdit, expected *yaml.Node) ([]byte, error) {
	body := bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var out bytes.Buffer
	if len(body) != len(raw) {
		out.Write([]byte{0xef, 0xbb, 0xbf})
	}
	offset := 0
	for _, edit := range edits {
		if edit.start < offset || edit.end < edit.start || edit.end > len(body) {
			return nil, errors.New("撤销位置无效，未修改配置")
		}
		out.Write(body[offset:edit.start])
		out.WriteString(edit.replacement)
		offset = edit.end
	}
	out.Write(body[offset:])
	candidate, err := parseYAMLDocument(out.Bytes())
	if err != nil {
		return nil, fmt.Errorf("撤销结果不是有效 YAML，未修改：%w", err)
	}
	if err = validateYAMLValues(candidate); err != nil {
		return nil, err
	}
	if !sameYAMLValue(expected, candidate.Content[0]) {
		return nil, errors.New("撤销结果影响了网卡字段以外的设置，已停止")
	}
	return out.Bytes(), nil
}

// 只对修复前后真正发生变化的字段做条件还原；其他当前字节保持不动。
func undoNetworkYAML(current, original []byte, adapter string) ([]byte, string, error) {
	applied, err := yamlEdit(original, adapter)
	if err != nil {
		return nil, "", err
	}
	if bytes.Equal(original, applied) {
		return nil, "", errors.New("该记录没有实际网卡修改，不能撤销")
	}
	oldDoc, _ := parseYAMLDocument(original)
	appliedDoc, _ := parseYAMLDocument(applied)
	nowDoc, err := parseYAMLDocument(current)
	if err != nil {
		return nil, "", err
	}
	root := nowDoc.Content[0]
	if root.Style&(yaml.FlowStyle|yaml.TaggedStyle) != 0 {
		return nil, "", errors.New("当前配置不是可安全局部撤销的块映射")
	}
	if err = validateEditableYAML(root); err != nil {
		return nil, "", err
	}
	if err = validateYAMLValues(nowDoc); err != nil {
		return nil, "", err
	}
	oldRoot, afterRoot := oldDoc.Content[0], appliedDoc.Content[0]
	_, oldTun := yamlField(oldRoot, "tun")
	_, afterTun := yamlField(afterRoot, "tun")
	tunKey, nowTun := yamlField(root, "tun")
	expected := cloneYAMLNode(root)
	body := bytes.TrimPrefix(current, []byte{0xef, 0xbb, 0xbf})
	lines, _ := yamlSourceLines(body)
	edits := []yamlTextEdit{}
	descriptions := []string{}
	change := func(parent, oldParent, afterParent, expectedParent *yaml.Node, keyName, label string) error {
		var before, after, node, key *yaml.Node
		if oldParent != nil {
			_, before = yamlField(oldParent, keyName)
		}
		if afterParent != nil {
			_, after = yamlField(afterParent, keyName)
		}
		if sameNetworkScalar(scalarState(before), scalarState(after)) {
			return nil
		}
		if parent != nil {
			key, node = yamlField(parent, keyName)
		}
		if !sameNetworkScalar(scalarState(node), scalarState(after)) {
			return fmt.Errorf("%s 与本工具写入的值不一致，可能已撤销或被再次修改；请核对后处理，未改动配置", label)
		}
		if parent == nil || expectedParent == nil || key == nil {
			return errors.New("当前网卡字段结构已变化，未修改")
		}
		var edit yamlTextEdit
		if before == nil {
			edit, err = deleteYAMLScalarLine(body, lines, key, node)
			undoField(expectedParent, keyName)
			descriptions = append(descriptions, label+"：删除本工具新增项")
		} else {
			var value string
			value, err = undoScalarText(scalarState(before))
			if err == nil {
				edit, err = yamlScalarEdit(body, lines, key, node, value)
			}
			setYAMLField(expectedParent, keyName, before.Tag, before.Value)
			descriptions = append(descriptions, label+"：恢复为 "+value)
		}
		if err != nil {
			return err
		}
		edits = append(edits, edit)
		return nil
	}
	if err = change(root, oldRoot, afterRoot, expected, "interface-name", "interface-name"); err != nil {
		return nil, "", err
	}
	_, expectedTun := yamlField(expected, "tun")
	var oldAuto *yaml.Node
	if oldTun != nil {
		_, oldAuto = yamlField(oldTun, "auto-detect-interface")
	}
	var afterAuto *yaml.Node
	if afterTun != nil {
		_, afterAuto = yamlField(afterTun, "auto-detect-interface")
	}
	autoChanged := !sameNetworkScalar(scalarState(oldAuto), scalarState(afterAuto))
	if autoChanged {
		if tunKey == nil || nowTun == nil || nowTun.Kind != yaml.MappingNode {
			return nil, "", errors.New("tun 结构已变化，不能安全撤销网卡字段")
		}
		_, _, headerValue, e := yamlTargetLine(body, lines, tunKey)
		if e != nil || headerValue.Tag != "!!null" || headerValue.Value != "" || nowTun.Style&(yaml.FlowStyle|yaml.TaggedStyle) != 0 {
			return nil, "", errors.New("tun 不是普通块映射，不能安全局部撤销")
		}
		if err = change(nowTun, oldTun, afterTun, expectedTun, "auto-detect-interface", "tun.auto-detect-interface"); err != nil {
			return nil, "", err
		}
		if len(expectedTun.Content) == 0 {
			if oldTun == nil {
				headerValue.Line = tunKey.Line
				edit, e := deleteYAMLScalarLine(body, lines, tunKey, headerValue)
				if e != nil {
					return nil, "", e
				}
				edits = append(edits, edit)
				undoField(expected, "tun")
			} else if oldTun.Kind == yaml.ScalarNode {
				setYAMLField(expected, "tun", "!!null", oldTun.Value)
			} else {
				headerValue.Line = tunKey.Line
				edit, e := yamlScalarEdit(body, lines, tunKey, headerValue, "{}")
				if e != nil {
					return nil, "", e
				}
				edits = append(edits, edit)
			}
		}
	}
	result, err := applyUndoEdits(current, edits, expected)
	return result, strings.Join(descriptions, "\r\n"), err
}

type yamlNetworkUndoPlan struct {
	restore     *yamlRestorePlan
	receiptPath string
	receiptData []byte
	receiptInfo os.FileInfo
	output      []byte
	summary     string
}

func prepareNetworkUndo(backup string) (*yamlNetworkUndoPlan, error) {
	plan, err := prepareYAMLRestore(backup)
	if err != nil {
		return nil, err
	}
	receiptPath := plan.backupPath + networkReceiptSuffix
	if err = ordinary(receiptPath); err != nil {
		return nil, fmt.Errorf("缺少或无法读取网卡撤销记录；旧版备份仍可使用整份恢复：%w", err)
	}
	file, err := os.Open(receiptPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	data, err := io.ReadAll(io.LimitReader(file, 4097))
	if err != nil {
		return nil, err
	}
	if len(data) > 4096 {
		return nil, errors.New("撤销记录过大，未修改")
	}
	var receipt networkReceipt
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&receipt); err != nil {
		return nil, errors.New("网卡撤销记录无效，未修改")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF || receipt.Format != 1 || receipt.BackupSHA256 != contentSHA256(plan.backupData) {
		return nil, errors.New("撤销记录与原备份不一致，未修改")
	}
	output, summary, err := undoNetworkYAML(plan.targetData, plan.backupData, receipt.Adapter)
	if err != nil {
		return nil, err
	}
	return &yamlNetworkUndoPlan{plan, receiptPath, data, info, output, summary}, nil
}
func checkNetworkUndoPlan(plan *yamlNetworkUndoPlan) error {
	if plan == nil || plan.restore == nil || plan.receiptInfo == nil {
		return errors.New("没有经过校验的网卡撤销计划")
	}
	if err := checkYAMLRestorePlan(plan.restore); err != nil {
		return err
	}
	if err := checkFileSnapshot(plan.receiptPath, plan.receiptInfo, plan.receiptData); err != nil {
		return err
	}
	validated, err := prepareNetworkUndo(plan.restore.backupPath)
	if err != nil {
		return err
	}
	if !bytes.Equal(validated.output, plan.output) {
		return errors.New("撤销计划已变化，请重新选择并确认")
	}
	return nil
}
func applyNetworkUndo(plan *yamlNetworkUndoPlan) (string, string, error) {
	if err := checkNetworkUndoPlan(plan); err != nil {
		return "", "", err
	}
	target := plan.restore.targetPath
	guard, err := openYAMLGuard(target)
	if err != nil {
		return "", "", err
	}
	defer guard.Close()
	if err = checkNetworkUndoPlan(plan); err != nil {
		return "", "", err
	}
	protect, err := backupFile(target)
	if err != nil {
		return "", "", err
	}
	saved, err := os.ReadFile(protect)
	if err != nil || !bytes.Equal(saved, plan.restore.targetData) {
		return "", protect, errors.New("撤销保护备份与当前快照不一致，已停止")
	}
	if err = checkNetworkUndoPlan(plan); err != nil {
		return "", protect, err
	}
	guard.closeSource()
	if err = checkNetworkUndoPlan(plan); err != nil {
		return "", protect, err
	}
	if err = replace(target, plan.output); err != nil {
		return "", protect, err
	}
	return target, protect, nil
}

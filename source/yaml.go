package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

// 解析只用于校验与定位。输出仍由原始字节上的局部补丁生成。
func parseYAMLDocument(raw []byte) (*yaml.Node, error) {
	if !utf8.Valid(raw) {
		return nil, errors.New("此 YAML 不是 UTF-8 编码，请从客户端导出 UTF-8 配置")
	}
	body := bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	if bytes.Contains(bytes.ReplaceAll(body, []byte("\r\n"), nil), []byte("\r")) {
		return nil, errors.New("不支持的换行格式，请使用 LF 或 CRLF")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(body))
	var document yaml.Node
	if err := decoder.Decode(&document); err != nil {
		if err == io.EOF {
			return nil, errors.New("空配置文件，不能修复或恢复")
		}
		return nil, fmt.Errorf("YAML 解析失败：%w", err)
	}
	if len(document.Content) != 1 || document.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("配置必须是单文档 YAML 映射")
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err != nil {
			return nil, fmt.Errorf("YAML 文档尾部无效：%w", err)
		}
		return nil, errors.New("不支持多文档 YAML，未修改文件")
	}
	return &document, nil
}

func validateYAMLValues(document *yaml.Node) error {
	var value interface{}
	if err := document.Decode(&value); err != nil {
		return fmt.Errorf("YAML 存在重复键或无效值：%w", err)
	}
	return nil
}

func validateEditableYAML(node *yaml.Node) error {
	if node.Anchor != "" || node.Kind == yaml.AliasNode {
		return errors.New("配置含 YAML 锚点或别名，无法安全局部修改")
	}
	switch node.Tag {
	case "!!map", "!!seq", "!!str", "!!null", "!!bool", "!!int", "!!float", "!!timestamp", "!!binary":
	default:
		return errors.New("配置含合并键或自定义 YAML 标签，无法安全局部修改")
	}
	if node.Kind == yaml.MappingNode {
		keys := make(map[string]bool)
		for i := 0; i < len(node.Content); i += 2 {
			key := node.Content[i]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return errors.New("配置映射键必须是字符串，不支持合并键或复杂键")
			}
			if keys[key.Value] {
				return fmt.Errorf("检测到重复 YAML 键：%s", key.Value)
			}
			keys[key.Value] = true
		}
	}
	for _, child := range node.Content {
		if err := validateEditableYAML(child); err != nil {
			return err
		}
	}
	return nil
}

func yamlField(mapping *yaml.Node, name string) (key, value *yaml.Node) {
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == name {
			return mapping.Content[i], mapping.Content[i+1]
		}
	}
	return nil, nil
}

type yamlSourceLine struct{ start, contentEnd, end int }
type yamlTextEdit struct {
	start, end  int
	replacement string
}

func yamlSourceLines(body []byte) ([]yamlSourceLine, string) {
	var lines []yamlSourceLine
	separator := ""
	for start := 0; start < len(body); {
		end := len(body)
		if offset := bytes.IndexByte(body[start:], '\n'); offset >= 0 {
			end = start + offset + 1
		}
		contentEnd := end
		if body[end-1] == '\n' {
			contentEnd--
			if contentEnd > start && body[contentEnd-1] == '\r' {
				contentEnd--
			}
			if separator == "" {
				separator = string(body[contentEnd:end])
			}
		}
		lines = append(lines, yamlSourceLine{start, contentEnd, end})
		start = end
	}
	if separator == "" {
		separator = "\n"
	}
	return lines, separator
}

func yamlNodeLine(body []byte, lines []yamlSourceLine, node *yaml.Node) (yamlSourceLine, string, error) {
	if node.Line < 1 || node.Line > len(lines) {
		return yamlSourceLine{}, "", errors.New("无法定位 YAML 节点，未修改文件")
	}
	line := lines[node.Line-1]
	return line, string(body[line.start:line.contentEnd]), nil
}

// 单独解析目标行，排除跨行引号、折叠的 plain scalar 及显式复杂键。
func yamlTargetLine(body []byte, lines []yamlSourceLine, key *yaml.Node) (yamlSourceLine, string, *yaml.Node, error) {
	line, text, err := yamlNodeLine(body, lines, key)
	if err != nil {
		return line, text, nil, err
	}
	if key.Style&(yaml.LiteralStyle|yaml.FoldedStyle|yaml.TaggedStyle) != 0 {
		return line, text, nil, errors.New("目标键使用不支持的 YAML 表示")
	}
	parsed, err := parseYAMLDocument([]byte(text))
	if err != nil || len(parsed.Content[0].Content) != 2 {
		return line, text, nil, errors.New("目标字段必须完整地位于同一文本行")
	}
	pair := parsed.Content[0].Content
	if pair[0].Value != key.Value || pair[0].Tag != "!!str" || strings.HasPrefix(strings.TrimSpace(text), "?") {
		return line, text, nil, errors.New("目标字段使用不支持的键表示")
	}
	return line, text, pair[1], nil
}

// 返回引号外的行尾注释位置。plain scalar 中的撇号不是引号起始符。
func yamlCommentStart(text string, start int) int {
	i := start
	if i < len(text) && (text[i] == '\'' || text[i] == '"') {
		quote := text[i]
		i++
		for i < len(text) {
			if quote == '"' && text[i] == '\\' {
				i += 2
				continue
			}
			if text[i] == quote {
				if quote == '\'' && i+1 < len(text) && text[i+1] == quote {
					i += 2
					continue
				}
				i++
				break
			}
			i++
		}
	}
	for ; i < len(text); i++ {
		if text[i] == '#' && (i == start || text[i-1] == ' ' || text[i-1] == '\t') {
			return i
		}
	}
	return len(text)
}

func yamlColon(text string) int {
	i := len(text) - len(strings.TrimLeft(text, " "))
	if i < len(text) && (text[i] == '\'' || text[i] == '"') {
		quote := text[i]
		i++
		for i < len(text) {
			if quote == '"' && text[i] == '\\' {
				i += 2
				continue
			}
			if text[i] == quote {
				if quote == '\'' && i+1 < len(text) && text[i+1] == quote {
					i += 2
					continue
				}
				i++
				break
			}
			i++
		}
	}
	for ; i < len(text); i++ {
		if text[i] == ':' {
			return i
		}
	}
	return -1
}

func yamlScalarEdit(body []byte, lines []yamlSourceLine, key, value *yaml.Node, replacement string) (yamlTextEdit, error) {
	line, text, single, err := yamlTargetLine(body, lines, key)
	if err != nil {
		return yamlTextEdit{}, err
	}
	if value.Kind != yaml.ScalarNode || value.Style&(yaml.LiteralStyle|yaml.FoldedStyle|yaml.TaggedStyle) != 0 || value.Line != key.Line || single.Tag != value.Tag || single.Value != value.Value {
		return yamlTextEdit{}, errors.New("目标值必须是同一行的普通标量，不支持块标量或跨行值")
	}
	colon := yamlColon(text)
	if colon < 0 {
		return yamlTextEdit{}, errors.New("无法定位目标字段冒号")
	}
	start := colon + 1
	for start < len(text) && (text[start] == ' ' || text[start] == '\t') {
		start++
	}
	end := yamlCommentStart(text, start)
	for end > start && (text[end-1] == ' ' || text[end-1] == '\t') {
		end--
	}
	if start == colon+1 {
		replacement = " " + replacement
	}
	if start == end && start < len(text) && text[start] == '#' {
		replacement += " "
	}
	return yamlTextEdit{line.start + start, line.start + end, replacement}, nil
}

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	copy := *node
	copy.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		copy.Content[i] = cloneYAMLNode(child)
	}
	return &copy
}

func setYAMLField(mapping *yaml.Node, name, tag, value string) {
	for i := 0; i < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == name {
			mapping.Content[i+1] = &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value}
			return
		}
	}
	mapping.Content = append(mapping.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: name}, &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: value})
}

func sameYAMLValue(a, b *yaml.Node) bool {
	if a.Kind != b.Kind || a.Tag != b.Tag || len(a.Content) != len(b.Content) {
		return false
	}
	if a.Kind == yaml.MappingNode {
		values := make(map[string]*yaml.Node)
		for i := 0; i < len(b.Content); i += 2 {
			values[b.Content[i].Value] = b.Content[i+1]
		}
		for i := 0; i < len(a.Content); i += 2 {
			value, ok := values[a.Content[i].Value]
			if !ok || !sameYAMLValue(a.Content[i+1], value) {
				return false
			}
		}
		return true
	}
	if a.Value != b.Value && !(a.Tag == "!!bool" && strings.EqualFold(a.Value, b.Value)) {
		return false
	}
	for i := range a.Content {
		if !sameYAMLValue(a.Content[i], b.Content[i]) {
			return false
		}
	}
	return true
}

func yamlEdit(raw []byte, adapter string) ([]byte, error) {
	if adapter == "" || !utf8.ValidString(adapter) || strings.ContainsAny(adapter, "\x00\n\r") {
		return nil, errors.New("网卡名称无效")
	}
	document, err := parseYAMLDocument(raw)
	if err != nil {
		return nil, err
	}
	root := document.Content[0]
	if root.Style&(yaml.FlowStyle|yaml.TaggedStyle) != 0 {
		return nil, errors.New("根节点必须使用普通块映射，无法安全修改行内或带标签的根节点")
	}
	if err = validateEditableYAML(root); err != nil {
		return nil, err
	}
	if err = validateYAMLValues(document); err != nil {
		return nil, err
	}
	body := bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf})
	lines, separator := yamlSourceLines(body)
	rootLine, rootText, err := yamlNodeLine(body, lines, root)
	if err != nil {
		return nil, err
	}
	rootIndent := len(rootText) - len(strings.TrimLeft(rootText, " "))
	interfaceKey, interfaceValue := yamlField(root, "interface-name")
	tunKey, tunValue := yamlField(root, "tun")
	var edits []yamlTextEdit
	if interfaceKey != nil {
		if interfaceValue.Tag != "!!str" && interfaceValue.Tag != "!!null" {
			return nil, errors.New("interface-name 必须是字符串或空值")
		}
		edit, err := yamlScalarEdit(body, lines, interfaceKey, interfaceValue, strconv.Quote(adapter))
		if err != nil {
			return nil, err
		}
		if interfaceValue.Tag != "!!str" || interfaceValue.Value != adapter {
			edits = append(edits, edit)
		}
	}
	if tunKey != nil {
		header, _, single, err := yamlTargetLine(body, lines, tunKey)
		if err != nil {
			return nil, err
		}
		if single.Kind != yaml.ScalarNode || single.Tag != "!!null" || single.Value != "" || single.Style != 0 || tunValue.Style&(yaml.FlowStyle|yaml.TaggedStyle) != 0 {
			return nil, errors.New("tun 必须是普通块映射或空块，不支持行内语法、标签或别名")
		}
		if tunValue.Kind != yaml.MappingNode && !(tunValue.Kind == yaml.ScalarNode && tunValue.Tag == "!!null" && tunValue.Value == "") {
			return nil, errors.New("tun 必须是映射，不能是序列或标量")
		}
		autoKey, autoValue := yamlField(tunValue, "auto-detect-interface")
		if autoKey != nil {
			if autoValue.Tag != "!!bool" && autoValue.Tag != "!!null" {
				return nil, errors.New("tun.auto-detect-interface 必须是布尔值或空值")
			}
			edit, err := yamlScalarEdit(body, lines, autoKey, autoValue, "false")
			if err != nil {
				return nil, err
			}
			if autoValue.Tag != "!!bool" || !strings.EqualFold(autoValue.Value, "false") {
				edits = append(edits, edit)
			}
		} else {
			indent := rootIndent + 2
			if len(tunValue.Content) > 0 {
				_, childText, err := yamlNodeLine(body, lines, tunValue.Content[0])
				if err != nil {
					return nil, err
				}
				indent = len(childText) - len(strings.TrimLeft(childText, " "))
			}
			ending := string(body[header.contentEnd:header.end])
			prefix := ""
			if ending == "" {
				prefix = separator
			}
			newLine := prefix + strings.Repeat(" ", indent) + "auto-detect-interface: false" + ending
			edits = append(edits, yamlTextEdit{header.end, header.end, newLine})
		}
	}
	var additions []string
	if interfaceKey == nil {
		additions = append(additions, strings.Repeat(" ", rootIndent)+"interface-name: "+strconv.Quote(adapter))
	}
	if tunKey == nil {
		additions = append(additions, strings.Repeat(" ", rootIndent)+"tun:", strings.Repeat(" ", rootIndent+2)+"auto-detect-interface: false")
	}
	if len(additions) > 0 {
		edits = append(edits, yamlTextEdit{rootLine.start, rootLine.start, strings.Join(additions, separator) + separator})
	}
	if len(edits) == 0 {
		return bytes.Clone(raw), nil
	}
	sort.Slice(edits, func(i, j int) bool { return edits[i].start < edits[j].start })
	var result bytes.Buffer
	if len(body) != len(raw) {
		result.Write([]byte{0xef, 0xbb, 0xbf})
	}
	offset := 0
	for _, edit := range edits {
		if edit.start < offset || edit.end < edit.start || edit.end > len(body) {
			return nil, errors.New("YAML 修改位置重叠或无效，未修改文件")
		}
		result.Write(body[offset:edit.start])
		result.WriteString(edit.replacement)
		offset = edit.end
	}
	result.Write(body[offset:])
	output := result.Bytes()
	candidate, err := parseYAMLDocument(output)
	if err != nil {
		return nil, fmt.Errorf("候选 YAML 未通过校验，未修改文件：%w", err)
	}
	if err = validateEditableYAML(candidate.Content[0]); err != nil {
		return nil, err
	}
	if err = validateYAMLValues(candidate); err != nil {
		return nil, err
	}
	expected := cloneYAMLNode(root)
	setYAMLField(expected, "interface-name", "!!str", adapter)
	_, expectedTun := yamlField(expected, "tun")
	if expectedTun == nil {
		expected.Content = append(expected.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "tun"}, &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"})
		_, expectedTun = yamlField(expected, "tun")
	}
	if expectedTun.Kind != yaml.MappingNode {
		*expectedTun = yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	}
	setYAMLField(expectedTun, "auto-detect-interface", "!!bool", "false")
	if !sameYAMLValue(expected, candidate.Content[0]) {
		return nil, errors.New("候选 YAML 改变了目标以外的配置或目标类型，未修改文件")
	}
	return output, nil
}

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"go.yaml.in/yaml/v3"
)

const (
	clientRev    = "rev"
	clientCFW    = "cfw"
	clientManual = "manual"
)

var proxyClientRunning = runningProxyClient

type searchRoot struct{ Path, Kind, Source string }
type pathCandidate struct {
	Path, Label, Note, Client string
	metadataPath              string
	metadataData              []byte
	metadataInfo              os.FileInfo
}
type pathSearchResult struct {
	Candidates []pathCandidate
	Note       string
}

func uniqueRoots(roots []searchRoot) []searchRoot {
	seen := map[string]bool{}
	out := []searchRoot{}
	for _, root := range roots {
		if strings.TrimSpace(root.Path) == "" {
			continue
		}
		abs, err := filepath.Abs(strings.Trim(strings.TrimSpace(root.Path), `"`))
		if err != nil {
			continue
		}
		key := root.Kind + "|" + strings.ToLower(filepath.Clean(abs))
		if !seen[key] {
			seen[key] = true
			root.Path = abs
			out = append(out, root)
		}
	}
	return out
}
func findDLLCandidates(ctx context.Context, roots []searchRoot) pathSearchResult {
	result := pathSearchResult{}
	seen := map[string]bool{}
	for _, root := range uniqueRoots(roots) {
		if ctx.Err() != nil {
			return result
		}
		dir := filepath.Join(root.Path, "daemon")
		if strings.EqualFold(filepath.Base(root.Path), "daemon") {
			dir = root.Path
		}
		for _, name := range []string{"wintun.dll", suffix} {
			path := filepath.Join(dir, name)
			key := strings.ToLower(path)
			if seen[key] {
				continue
			}
			seen[key] = true
			if ordinary(path) != nil || checkDLLPath(path) != nil || isWindowsBinary(path) != nil {
				continue
			}
			state := "可修复"
			if name == suffix {
				state = "可恢复"
			}
			result.Candidates = append(result.Candidates, pathCandidate{Path: path, Label: state + " · " + path, Note: root.Source})
		}
	}
	if len(result.Candidates) == 0 {
		result.Note = "未找到可用的 OPPO DLL。请先启动 OPPO 互联后重新查找，或使用‘手动选择’。"
	}
	return result
}

func readDiscoveryYAML(path string) ([]byte, os.FileInfo, *yaml.Node, error) {
	if err := ordinary(path); err != nil {
		return nil, nil, nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, nil, err
	}
	raw, err := io.ReadAll(io.LimitReader(f, (2<<20)+1))
	if err != nil {
		return nil, nil, nil, err
	}
	if len(raw) > 2<<20 {
		return nil, nil, nil, errors.New("配置索引过大，不能自动识别")
	}
	doc, err := parseYAMLDocument(raw)
	if err != nil {
		return nil, nil, nil, err
	}
	if err = validateYAMLValues(doc); err != nil {
		return nil, nil, nil, err
	}
	return raw, info, doc, nil
}
func cfwCurrentTarget(root string) (pathCandidate, error) {
	indexPath := filepath.Join(root, "profiles", "list.yml")
	raw, info, doc, err := readDiscoveryYAML(indexPath)
	if err != nil {
		return pathCandidate{}, err
	}
	var index struct {
		Index *int `yaml:"index"`
		Files []struct {
			Time string `yaml:"time"`
		} `yaml:"files"`
	}
	if err = doc.Decode(&index); err != nil {
		return pathCandidate{}, errors.New("CFW 配置索引格式无法识别")
	}
	if index.Index == nil || *index.Index < 0 || *index.Index >= len(index.Files) {
		return pathCandidate{}, errors.New("请先在 Clash for Windows 的 Profiles 中启用一份配置，再完全退出软件并重新识别")
	}
	name := index.Files[*index.Index].Time
	if name == "" || filepath.Base(name) != name || strings.ContainsAny(name, `/\:`) || validateYAMLPath(name) != nil {
		return pathCandidate{}, errors.New("CFW 当前配置文件名无效，已停止自动选择")
	}
	path := filepath.Join(root, "profiles", name)
	if ordinary(path) != nil {
		return pathCandidate{}, errors.New("CFW 当前配置文件不存在，请在客户端重新启用配置")
	}
	return pathCandidate{Path: path, Client: clientCFW, Label: "Clash for Windows", Note: "当前启用的配置；订阅刷新可能重置这项修改。修复后重新打开客户端。", metadataPath: indexPath, metadataData: raw, metadataInfo: info}, nil
}
func vergeConfigTarget(root string) (pathCandidate, error) {
	// config.yaml 是 Rev 持久程序配置，不选择 clash-verge.yaml 等生成文件。
	if _, _, _, err := readDiscoveryYAML(filepath.Join(root, "verge.yaml")); err != nil {
		return pathCandidate{}, errors.New("没有找到可识别的 Clash Verge Rev 数据目录")
	}
	path := filepath.Join(root, "config.yaml")
	if _, _, _, err := readDiscoveryYAML(path); err != nil {
		return pathCandidate{}, errors.New("Clash Verge Rev 程序配置无效或不可读取")
	}
	return pathCandidate{Path: path, Client: clientRev, Label: "Clash Verge Rev", Note: "已识别程序配置。请完全退出 Clash Verge Rev，修复后重新打开；扩展脚本仍可能覆盖网卡设置。"}, nil
}
func findProxyCandidates(ctx context.Context, roots []searchRoot) pathSearchResult {
	result := pathSearchResult{}
	found := map[string]bool{}
	problems := map[string]string{}
	for _, root := range uniqueRoots(roots) {
		if ctx.Err() != nil {
			return result
		}
		if _, err := os.Stat(root.Path); err != nil {
			continue
		}
		var candidate pathCandidate
		var err error
		switch root.Kind {
		case clientRev:
			candidate, err = vergeConfigTarget(root.Path)
		case clientCFW:
			candidate, err = cfwCurrentTarget(root.Path)
		default:
			continue
		}
		if err != nil {
			if problems[root.Kind] == "" {
				problems[root.Kind] = err.Error()
			}
			continue
		}
		candidate.Label += " · " + root.Path
		result.Candidates = append(result.Candidates, candidate)
		found[root.Kind] = true
	}
	for _, kind := range []string{clientRev, clientCFW} {
		if found[kind] {
			continue
		}
		name := "Clash Verge Rev"
		if kind == clientCFW {
			name = "Clash for Windows"
		}
		note := problems[kind]
		if note == "" {
			note = "未找到 " + name + " 的配置目录。请先运行一次客户端，或手动指定配置。"
		}
		result.Candidates = append(result.Candidates, pathCandidate{Client: kind, Label: name + "（需要设置）", Note: note})
	}
	return result
}
func verifyProxyTarget(candidate *pathCandidate) error {
	if candidate == nil || candidate.Path == "" {
		return errors.New("请先选择已识别的代理客户端或手动指定配置")
	}
	if err := validateYAMLPath(candidate.Path); err != nil {
		return err
	}
	if candidate.metadataPath != "" {
		raw, info, _, err := readDiscoveryYAML(candidate.metadataPath)
		if err != nil || !os.SameFile(info, candidate.metadataInfo) || !bytes.Equal(raw, candidate.metadataData) {
			return errors.New("客户端当前配置已经变化，请重新识别并选择；未修改文件")
		}
	}
	if proxyClientRunning(candidate.Client) {
		return fmt.Errorf("请先完全退出 %s（包括托盘程序），再执行修复；修复后重新打开", clientDisplayName(candidate.Client))
	}
	return nil
}
func clientDisplayName(kind string) string {
	if kind == clientRev {
		return "Clash Verge Rev"
	}
	if kind == clientCFW {
		return "Clash for Windows"
	}
	return "代理客户端"
}

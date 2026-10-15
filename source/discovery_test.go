package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeDiscoveryFile(t *testing.T, path string, raw []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestDLLAutoDiscoveryIsScopedAndReadOnly(t *testing.T) {
	root := filepath.Join(t.TempDir(), "自定义安装位置", "OPPO Connect")
	path := filepath.Join(root, "daemon", "wintun.dll")
	image := testDLLImage()
	writeDiscoveryFile(t, path, image)
	writeDiscoveryFile(t, filepath.Join(root, "other", "wintun.dll"), image)
	invalid := filepath.Join(t.TempDir(), "fake-oppo")
	writeDiscoveryFile(t, filepath.Join(invalid, "daemon", "wintun.dll"), []byte("MZ"))
	result := findDLLCandidates(context.Background(), []searchRoot{{root, "dll", "安装记录"}, {root, "dll", "重复记录"}, {invalid, "dll", "常见目录"}})
	if len(result.Candidates) != 1 || result.Candidates[0].Path != path {
		t.Fatal("未精确定位 daemon DLL", result)
	}
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, image) {
		t.Fatal("查找修改了 DLL")
	}
	changed := filepath.Join(root, "daemon", suffix)
	if err := os.Rename(path, changed); err != nil {
		t.Fatal(err)
	}
	result = findDLLCandidates(context.Background(), []searchRoot{{filepath.Dir(changed), "dll", "进程目录"}})
	if len(result.Candidates) != 1 || result.Candidates[0].Path != changed {
		t.Fatal("未识别可恢复文件")
	}
}
func TestProxyDiscoverySupportsBothClients(t *testing.T) {
	base := t.TempDir()
	rev := filepath.Join(base, "Verge Rev")
	cfw := filepath.Join(base, "CFW")
	writeDiscoveryFile(t, filepath.Join(rev, "verge.yaml"), []byte("enable_tun_mode: true\n"))
	writeDiscoveryFile(t, filepath.Join(rev, "config.yaml"), []byte("tun:\n  auto-detect-interface: true\n"))
	writeDiscoveryFile(t, filepath.Join(rev, "clash-verge.yaml"), []byte("tun: {enable: true}\n"))
	writeDiscoveryFile(t, filepath.Join(cfw, "profiles", "list.yml"), []byte("index: 1\nfiles:\n  - time: old.yml\n  - time: current.yml\n"))
	writeDiscoveryFile(t, filepath.Join(cfw, "profiles", "old.yml"), []byte("mode: global\n"))
	writeDiscoveryFile(t, filepath.Join(cfw, "profiles", "current.yml"), []byte("mode: rule\n"))
	result := findProxyCandidates(context.Background(), []searchRoot{{rev, clientRev, "默认目录"}, {cfw, clientCFW, "默认目录"}})
	if len(result.Candidates) != 2 {
		t.Fatal(result)
	}
	for _, c := range result.Candidates {
		switch c.Client {
		case clientRev:
			if c.Path != filepath.Join(rev, "config.yaml") {
				t.Fatal("选择了 Rev 临时运行文件")
			}
		case clientCFW:
			if c.Path != filepath.Join(cfw, "profiles", "current.yml") {
				t.Fatal("没有按 CFW 当前索引选择")
			}
		default:
			t.Fatal(c)
		}
	}
}
func TestCFWDiscoveryRefusesAmbiguousOrUnsafeIndex(t *testing.T) {
	for name, metadata := range map[string]string{
		"none-selected": "index: -1\nfiles:\n  - time: current.yml\n",
		"missing-index": "files:\n  - time: current.yml\n",
		"out-of-range":  "index: 2\nfiles:\n  - time: current.yml\n",
		"traversal":     "index: 0\nfiles:\n  - time: ../other.yml\n",
		"duplicate":     "index: 0\nindex: 1\nfiles: []\n",
		"missing-file":  "index: 0\nfiles:\n  - time: missing.yml\n",
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeDiscoveryFile(t, filepath.Join(root, "profiles", "list.yml"), []byte(metadata))
			writeDiscoveryFile(t, filepath.Join(root, "profiles", "current.yml"), []byte("mode: rule\n"))
			if _, err := cfwCurrentTarget(root); err == nil {
				t.Fatal("错误索引被自动选择")
			}
		})
	}
}
func TestCFWDiscoveredTargetChangesAbort(t *testing.T) {
	old := proxyClientRunning
	proxyClientRunning = func(string) bool { return false }
	defer func() { proxyClientRunning = old }()
	root := t.TempDir()
	index := filepath.Join(root, "profiles", "list.yml")
	writeDiscoveryFile(t, index, []byte("index: 0\nfiles:\n  - time: a.yml\n  - time: b.yml\n"))
	writeDiscoveryFile(t, filepath.Join(root, "profiles", "a.yml"), []byte("mode: rule\n"))
	candidate, err := cfwCurrentTarget(root)
	if err != nil {
		t.Fatal(err)
	}
	if err = verifyProxyTarget(&candidate); err != nil {
		t.Fatal(err)
	}
	writeDiscoveryFile(t, index, []byte("index: 1\nfiles:\n  - time: a.yml\n  - time: b.yml\n"))
	if err = verifyProxyTarget(&candidate); err == nil {
		t.Fatal("客户端切换配置后仍使用旧目标")
	}
}
func TestProxyRepairRequiresClientExit(t *testing.T) {
	old := proxyClientRunning
	proxyClientRunning = func(kind string) bool { return kind == clientRev || kind == clientCFW }
	defer func() { proxyClientRunning = old }()
	for _, kind := range []string{clientRev, clientCFW} {
		candidate := pathCandidate{Path: filepath.Join(t.TempDir(), "config.yaml"), Client: kind}
		if err := verifyProxyTarget(&candidate); err == nil || !strings.Contains(err.Error(), "完全退出") {
			t.Fatal("没有保护正在运行的客户端", err)
		}
	}
}

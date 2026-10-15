package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// 修复、删除旧工具、使用另一份工具恢复分别发生在不同进程，不继承内存状态。
func TestRestoreAfterToolDeletion(t *testing.T) {
	if dir := os.Getenv("OPPO_REPAIR_RECOVERY_TEST_DIR"); dir != "" {
		dll := filepath.Join(dir, "OPPO", "daemon", "wintun.dll")
		yaml := filepath.Join(dir, "config.yaml")
		switch os.Getenv("OPPO_REPAIR_RECOVERY_TEST_PHASE") {
		case "repair":
			if _, err := changeDLL(dll, false); err != nil {
				t.Fatal(err)
			}
			if _, err := updateYaml(yaml, "WLAN"); err != nil {
				t.Fatal(err)
			}
		case "restore":
			found := findDLLCandidates(context.Background(), []searchRoot{{Path: filepath.Join(dir, "OPPO")}})
			if len(found.Candidates) != 1 || filepath.Base(found.Candidates[0].Path) != suffix {
				t.Fatal("新进程无法识别已改名 DLL")
			}
			if _, err := changeDLL(found.Candidates[0].Path, true); err != nil {
				t.Fatal(err)
			}
			backups, err := filepath.Glob(yaml + ".oppo-backup-*.bak")
			if err != nil || len(backups) != 1 {
				t.Fatal("未找到原配置旁的备份", err, backups)
			}
			if _, _, err := restoreYaml(backups[0]); err != nil {
				t.Fatal(err)
			}
		default:
			t.Fatal("未知测试阶段")
		}
		return
	}
	dir := t.TempDir()
	dll, yaml := filepath.Join(dir, "OPPO", "daemon", "wintun.dll"), filepath.Join(dir, "config.yaml")
	dllBytes, yamlBytes := testDLLImage(), []byte("port: 7890\r\ntun:\r\n  auto-detect-interface: true\r\n")
	writeDiscoveryFile(t, dll, dllBytes)
	writeDiscoveryFile(t, yaml, yamlBytes)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	toolBytes, err := os.ReadFile(self)
	if err != nil {
		t.Fatal(err)
	}
	for _, phase := range []string{"repair", "restore"} {
		tool := filepath.Join(dir, phase+"-tool.exe")
		if err := os.WriteFile(tool, toolBytes, 0700); err != nil {
			t.Fatal(err)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		cmd := exec.CommandContext(ctx, tool, "-test.run=^TestRestoreAfterToolDeletion$", "-test.count=1")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "OPPO_REPAIR_RECOVERY_TEST_DIR="+dir, "OPPO_REPAIR_RECOVERY_TEST_PHASE="+phase)
		output, err := cmd.CombinedOutput()
		cancel()
		if err != nil {
			t.Fatalf("%s 子进程失败：%v\n%s", phase, err, output)
		}
		if err := os.Remove(tool); err != nil {
			t.Fatal("无法删除已退出的测试工具", err)
		}
	}
	for path, expected := range map[string][]byte{dll: dllBytes, yaml: yamlBytes} {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, expected) {
			t.Fatal("新工具恢复后原始字节不一致", path, err)
		}
	}
}

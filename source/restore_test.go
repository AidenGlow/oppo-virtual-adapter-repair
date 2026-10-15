package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func restoreFixture(t *testing.T, backupData, targetData []byte) (string, string) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "merge.yaml")
	backup := target + ".oppo-backup-20261015-010203.004.bak"
	if err := os.WriteFile(target, targetData, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(backup, backupData, 0600); err != nil {
		t.Fatal(err)
	}
	return target, backup
}

func TestRestorePreviewReadOnly(t *testing.T) {
	current, previous := []byte("port: 7890\n"), []byte("port: 7891\n")
	target, backup := restoreFixture(t, previous, current)
	dir := filepath.Dir(target)
	base, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	input := backup
	if relative, err := filepath.Rel(base, backup); err == nil {
		input = relative
	}
	plan, err := prepareYAMLRestore(input)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(plan.backupPath) || !filepath.IsAbs(plan.targetPath) || plan.backupPath != backup || plan.targetPath != target {
		t.Fatal("预览未固定完整备份和目标路径")
	}
	// 预览后取消：不调用提交函数，不应生成保护备份或写入文件。
	a, _ := os.ReadFile(target)
	b, _ := os.ReadFile(backup)
	files, _ := os.ReadDir(dir)
	if !bytes.Equal(a, current) || !bytes.Equal(b, previous) || len(files) != 2 {
		t.Fatal("准备恢复产生文件副作用")
	}
}

func TestRestorePlanFileChangesAbort(t *testing.T) {
	for _, change := range []string{"target_content", "backup_content", "target_identity", "backup_identity"} {
		t.Run(change, func(t *testing.T) {
			current, previous := []byte("port: 7890\n"), []byte("port: 7891\n")
			target, backup := restoreFixture(t, previous, current)
			plan, err := prepareYAMLRestore(backup)
			if err != nil {
				t.Fatal(err)
			}
			path, expectedTarget, expectedBackup := target, current, previous
			switch change {
			case "target_content":
				expectedTarget = []byte("port: 7892\n")
				if err := os.WriteFile(target, expectedTarget, 0600); err != nil {
					t.Fatal(err)
				}
			case "backup_content":
				expectedBackup = []byte("port: 7893\n")
				if err := os.WriteFile(backup, expectedBackup, 0600); err != nil {
					t.Fatal(err)
				}
			case "backup_identity":
				path = backup
			}
			if change == "target_identity" || change == "backup_identity" {
				data := current
				if path == backup {
					data = previous
				}
				replacement := filepath.Join(filepath.Dir(target), "replacement.tmp")
				if err := os.WriteFile(replacement, data, 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, path); err != nil {
					t.Fatal(err)
				}
			}
			dest, safety, err := applyYAMLRestore(plan)
			if err == nil || dest != "" || safety != "" {
				t.Fatal("确认前后的文件变化没有阻止提交")
			}
			a, _ := os.ReadFile(target)
			b, _ := os.ReadFile(backup)
			files, _ := os.ReadDir(filepath.Dir(target))
			if !bytes.Equal(a, expectedTarget) || !bytes.Equal(b, expectedBackup) || len(files) != 2 {
				t.Fatal("失效计划仍修改文件")
			}
		})
	}
}

func TestRestoreValidLegacyBackupAndCorruptCurrent(t *testing.T) {
	for name, previous := range map[string][]byte{
		"bom_crlf": []byte("\xef\xbb\xbfport: 7891\r\ntun:\r\n    enable: true\r\n"),
		"flow":     []byte("{port: 7891, tun: {enable: true}}\n"),
		"anchors":  []byte("defaults: &d\n  mtu: 9000\ntun:\n  <<: *d\n  enable: true\n"),
	} {
		t.Run(name, func(t *testing.T) {
			broken := []byte("rules: [\n")
			target, backup := restoreFixture(t, previous, broken)
			dest, safety, err := restoreYaml(backup)
			if err != nil || dest != target || safety == "" {
				t.Fatalf("恢复有效旧备份失败：%v", err)
			}
			actual, _ := os.ReadFile(target)
			protected, _ := os.ReadFile(safety)
			if !bytes.Equal(actual, previous) || !bytes.Equal(protected, broken) {
				t.Fatal("恢复或保护备份内容不一致")
			}
		})
	}
}

func TestRestoreRejectsInvalidPlansAndPaths(t *testing.T) {
	if _, _, err := applyYAMLRestore(nil); err == nil {
		t.Fatal("接受空计划")
	}
	for _, name := range []string{"file.txt.oppo-backup-20261015-010203.004.bak", "file.txt:stream.yaml.oppo-backup-20261015-010203.004.bak", "file.yaml.bak", "file.yaml.oppo-backup-not-a-date.bak"} {
		if _, _, err := restoreTargetPath(name); err == nil {
			t.Fatalf("接受不合法恢复路径 %q", name)
		}
	}
	for _, name := range []string{"file.YAML.oppo-backup-20261015-010203.004.bak", "file.yml.oppo-backup-20261015-010203.004-1.BAK"} {
		if _, _, err := restoreTargetPath(name); err != nil {
			t.Fatalf("拒绝有效备份格式：%v", err)
		}
	}
	_, backup := restoreFixture(t, []byte("port: 1\n"), []byte("port: 2\n"))
	plan, err := prepareYAMLRestore(backup)
	if err != nil {
		t.Fatal(err)
	}
	plan.targetPath = filepath.Join(filepath.Dir(backup), "unrelated.txt")
	if _, _, err := applyYAMLRestore(plan); err == nil {
		t.Fatal("接受目标关系被改写的计划")
	}
}

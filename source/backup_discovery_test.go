package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func backupDiscoveryFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func TestFindYAMLBackupsSortByTimestampAndSequence(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "merge.yaml")
	backupDiscoveryFile(t, target, []byte("port: 7890\n"))
	// 顺序也是预期的最终排序；同时间的序号按数值而非字符串比较。
	tags := []string{
		"20261016-010203.004",
		"20261015-010203.004-10",
		"20261015-010203.004-2",
		"20261015-010203.004-01",
		"20261015-010203.004-1",
		"20261015-010203.004",
		"20261014-235959.999",
	}
	paths := make([]string, len(tags))
	for i, tag := range tags {
		paths[i] = target + ".oppo-backup-" + tag + ".bak"
		backupDiscoveryFile(t, paths[i], []byte(strings.Repeat("x", i+1)))
		// 文件修改时间与备份时间的顺序相反，不能用 mtime 排序。
		mtime := time.Date(2030, time.January, i+1, 0, 0, 0, 0, time.Local)
		if err := os.Chtimes(paths[i], mtime, mtime); err != nil {
			t.Fatal(err)
		}
	}
	got, err := findYAMLBackups(target)
	if err != nil || len(got) != len(paths) {
		t.Fatalf("备份数量不符：%d，%v", len(got), err)
	}
	sequences := []int{0, 10, 2, 1, 1, 0, 0}
	for i, backup := range got {
		if backup.Path != paths[i] || !filepath.IsAbs(backup.Path) || backup.Sequence != sequences[i] || backup.Size != int64(i+1) {
			t.Fatalf("第 %d 项排序或元数据错误：%+v", i, backup)
		}
		if backup.Time.Format(backupTimeLayout) != tags[i][:len(backupTimeLayout)] {
			t.Fatalf("备份时间未取自文件名：%v", backup.Time)
		}
	}
	if got[1].Time.Format(backupTimeLayout) != "20261015-010203.004" {
		t.Fatal("带序号备份的时间解析错误")
	}
}

func TestFindYAMLBackupsMatchesOnlyCurrentTarget(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "merge.YAML")
	backupDiscoveryFile(t, target, []byte("port: 7890\n"))
	wanted := filepath.Join(dir, "MERGE.yaml.OPPO-BACKUP-20261015-010203.004.BAK")
	backupDiscoveryFile(t, wanted, []byte("not parsed during discovery"))
	for _, name := range []string{
		"other.yaml.oppo-backup-20261015-010203.004.bak",
		"prefix-merge.YAML.oppo-backup-20261015-010203.004.bak",
		"merge.YAML.extra.oppo-backup-20261015-010203.004.bak",
		"merge.yml.oppo-backup-20261015-010203.004.bak",
	} {
		backupDiscoveryFile(t, filepath.Join(dir, name), []byte("port: 1\n"))
	}
	subdir := filepath.Join(dir, "nested")
	if err := os.Mkdir(subdir, 0700); err != nil {
		t.Fatal(err)
	}
	backupDiscoveryFile(t, filepath.Join(subdir, "merge.YAML.oppo-backup-20261016-010203.004.bak"), []byte("port: 2\n"))
	got, err := findYAMLBackups(target)
	if err != nil || len(got) != 1 || got[0].Path != wanted {
		t.Fatalf("混入其他配置或子目录的备份：%+v，%v", got, err)
	}
	if _, err := prepareYAMLRestore(got[0].Path); err == nil {
		t.Fatal("发现阶段允许列出的损坏内容必须仍由恢复计划拒绝")
	}
}

func TestFindYAMLBackupsSkipsInvalidNamesAndDirectories(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yml")
	backupDiscoveryFile(t, target, []byte("port: 7890\n"))
	for _, tag := range []string{
		"20260229-010203.004", "20261301-010203.004", "20260001-010203.004",
		"20260132-010203.004", "20261015-240000.000", "20261015-016000.000",
		"20261015-010260.000", "20261015-010203.04", "20261015-010203.0004",
		"20261015-010203", "20261015-010203.004-", "20261015-010203.004--1",
		"20261015-010203.004-9999999999999999999999999999999", "00000101-010203.004",
	} {
		backupDiscoveryFile(t, target+".oppo-backup-"+tag+".bak", []byte("port: 1\n"))
	}
	for _, tail := range []string{".bak.tmp", ".bak.old", ".bakk", ".bak.bak"} {
		backupDiscoveryFile(t, target+".oppo-backup-20261015-010203.004"+tail, []byte("port: 1\n"))
	}
	if err := os.Mkdir(target+".oppo-backup-20261015-010203.004.bak", 0700); err != nil {
		t.Fatal(err)
	}
	wanted := target + ".oppo-backup-20240229-010203.004.bak"
	backupDiscoveryFile(t, wanted, []byte("port: 2\n"))
	got, err := findYAMLBackups(target)
	if err != nil || len(got) != 1 || got[0].Path != wanted {
		t.Fatalf("没有过滤非法日期、格式或目录：%+v，%v", got, err)
	}
}

func TestFindYAMLBackupsNoMatches(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	backupDiscoveryFile(t, target, []byte("port: 7890\n"))
	backupDiscoveryFile(t, filepath.Join(dir, "another.yaml.oppo-backup-20261015-010203.004.bak"), []byte("port: 1\n"))
	got, err := findYAMLBackups(target)
	if err != nil || len(got) != 0 {
		t.Fatalf("无匹配备份时应返回空列表：%+v，%v", got, err)
	}
}

func TestFindYAMLBackupsRejectsInvalidTarget(t *testing.T) {
	dir := t.TempDir()
	badFile := filepath.Join(dir, "config.txt")
	backupDiscoveryFile(t, badFile, []byte("port: 7890\n"))
	directory := filepath.Join(dir, "directory.yaml")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"", "  ", filepath.Join(dir, "missing.yaml"), badFile, directory, filepath.Join(dir, "stream.yaml:stream")} {
		if _, err := findYAMLBackups(path); err == nil {
			t.Fatalf("接受无效目标 %q", path)
		}
	}
}

func TestFindYAMLBackupsReadOnly(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	backup := target + ".oppo-backup-20261015-010203.004.bak"
	before := map[string][]byte{target: []byte("port: 7890\n"), backup: []byte("port: 7891\n"), filepath.Join(dir, "untouched.txt"): []byte("unchanged")}
	for path, data := range before {
		backupDiscoveryFile(t, path, data)
	}
	initialEntries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	initialTimes := make(map[string]time.Time)
	for path := range before {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		initialTimes[path] = info.ModTime()
	}
	for i := 0; i < 2; i++ {
		if _, err := findYAMLBackups(target); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	names := func(entries []os.DirEntry) []string {
		out := make([]string, len(entries))
		for i, entry := range entries {
			out[i] = entry.Name()
		}
		return out
	}
	if !reflect.DeepEqual(names(entries), names(initialEntries)) {
		t.Fatal("扫描新增或移除了文件")
	}
	for path, data := range before {
		actual, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(actual, data) {
			t.Fatalf("扫描改变文件内容：%s，%v", path, err)
		}
		info, err := os.Stat(path)
		if err != nil || !info.ModTime().Equal(initialTimes[path]) {
			t.Fatalf("扫描改变文件修改时间：%s，%v", path, err)
		}
	}
}

func TestFindYAMLBackupsSkipsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	backupDiscoveryFile(t, target, []byte("port: 7890\n"))
	linked := filepath.Join(dir, "real-backup.txt")
	backupDiscoveryFile(t, linked, []byte("port: 7891\n"))
	link := target + ".oppo-backup-20261015-010203.004.bak"
	if err := os.Symlink(linked, link); err != nil {
		t.Skipf("当前环境不能建立符号链接：%v", err)
	}
	got, err := findYAMLBackups(target)
	if err != nil || len(got) != 0 {
		t.Fatalf("扫描接受符号链接备份：%+v，%v", got, err)
	}
	alias := filepath.Join(dir, "alias.yaml")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	if _, err := findYAMLBackups(alias); err == nil {
		t.Fatal("扫描接受符号链接目标")
	}
}

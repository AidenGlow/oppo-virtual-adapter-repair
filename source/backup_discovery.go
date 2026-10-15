package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// YAMLBackup 只保存同目录备份的元数据；内容在用户选中后再校验。
type YAMLBackup struct {
	Path     string
	Time     time.Time
	Sequence int
	Size     int64
}

const backupTimeLayout = "20060102-150405.000"

var backupNameRE = regexp.MustCompile(`(?i)^(.+)\.oppo-backup-(\d{8}-\d{6}\.\d{3})(?:-(\d+))?\.bak$`)

// findYAMLBackups 只读当前配置所在目录，按备份名称内的时间列出对应备份。
// 不依赖文件修改时间，也不在扫描阶段读取 YAML 内容或建立保护备份。
func findYAMLBackups(target string) ([]YAMLBackup, error) {
	if strings.TrimSpace(target) == "" {
		return nil, errors.New("请先选择当前 YAML 配置")
	}
	abs, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return nil, err
	}
	if err := validateYAMLPath(abs); err != nil {
		return nil, err
	}
	if err := ordinary(abs); err != nil {
		return nil, fmt.Errorf("当前 YAML 配置不可用：%w", err)
	}
	dir, base := filepath.Dir(abs), filepath.Base(abs)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("读取配置备份目录失败：%w", err)
	}
	backups := make([]YAMLBackup, 0)
	for _, entry := range entries {
		parts := backupNameRE.FindStringSubmatch(entry.Name())
		if parts == nil || !strings.EqualFold(parts[1], base) {
			continue
		}
		stamp, err := time.ParseInLocation(backupTimeLayout, parts[2], time.Local)
		if err != nil || stamp.Year() == 0 || stamp.Format(backupTimeLayout) != parts[2] {
			continue
		}
		sequence := 0
		if parts[3] != "" {
			sequence, err = strconv.Atoi(parts[3])
			if err != nil {
				continue
			}
		}
		path := filepath.Join(dir, entry.Name())
		_, restored, err := restoreTargetPath(path)
		if err != nil || !strings.EqualFold(restored, abs) {
			continue
		}
		if err := ordinary(path); err != nil {
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			continue
		}
		backups = append(backups, YAMLBackup{Path: path, Time: stamp, Sequence: sequence, Size: info.Size()})
	}
	sort.Slice(backups, func(i, j int) bool {
		a, b := backups[i], backups[j]
		if !a.Time.Equal(b.Time) {
			return a.Time.After(b.Time)
		}
		if a.Sequence != b.Sequence {
			return a.Sequence > b.Sequence
		}
		aPath, bPath := strings.ToLower(a.Path), strings.ToLower(b.Path)
		if aPath != bPath {
			return aPath < bPath
		}
		return a.Path < b.Path
	})
	return backups, nil
}

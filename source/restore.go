package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func validateYAMLPath(path string) error {
	ext := strings.ToLower(filepath.Ext(path))
	if (ext != ".yaml" && ext != ".yml") || strings.ContainsAny(filepath.Base(path), ":\x00") {
		return errors.New("目标必须是普通 .yaml/.yml 文件，不允许其他类型或备用数据流")
	}
	return nil
}

type yamlRestorePlan struct {
	backupPath, targetPath string
	backupData, targetData []byte
	backupInfo, targetInfo os.FileInfo
}

func restoreTargetPath(backup string) (string, string, error) {
	abs, err := filepath.Abs(filepath.Clean(backup))
	if err != nil {
		return "", "", err
	}
	if backup == "" || !suffixRE.MatchString(filepath.Base(abs)) {
		return "", "", errors.New("请选择时间戳格式正确的 YAML 备份文件")
	}
	target := suffixRE.ReplaceAllString(abs, "")
	if err := validateYAMLPath(target); err != nil {
		return "", "", err
	}
	return abs, target, nil
}

func readRestoreFile(path string) ([]byte, os.FileInfo, error) {
	if err := ordinary(path); err != nil {
		return nil, nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, nil, errors.New("恢复文件不是普通文件")
	}
	raw, err := io.ReadAll(f)
	return raw, info, err
}

// 准备阶段只读文件；GUI 确认的是这份已校验计划中的路径。
func prepareYAMLRestore(backup string) (*yamlRestorePlan, error) {
	backupPath, targetPath, err := restoreTargetPath(backup)
	if err != nil {
		return nil, err
	}
	backupData, backupInfo, err := readRestoreFile(backupPath)
	if err != nil {
		return nil, fmt.Errorf("读取备份失败：%w", err)
	}
	document, err := parseYAMLDocument(backupData)
	if err != nil {
		return nil, fmt.Errorf("备份不是有效的 YAML 配置：%w", err)
	}
	if err := validateYAMLValues(document); err != nil {
		return nil, fmt.Errorf("备份配置校验失败：%w", err)
	}
	targetData, targetInfo, err := readRestoreFile(targetPath)
	if err != nil {
		return nil, fmt.Errorf("读取恢复目标失败：%w", err)
	}
	return &yamlRestorePlan{backupPath, targetPath, backupData, targetData, backupInfo, targetInfo}, nil
}

func checkYAMLRestorePlan(plan *yamlRestorePlan) error {
	if plan == nil || plan.backupInfo == nil || plan.targetInfo == nil {
		return errors.New("没有经过校验的恢复计划")
	}
	backup, target, err := restoreTargetPath(plan.backupPath)
	if err != nil || backup != plan.backupPath || target != plan.targetPath {
		return errors.New("恢复计划的文件关系无效，请重新选择备份")
	}
	for _, file := range []struct {
		path string
		data []byte
		info os.FileInfo
	}{
		{plan.backupPath, plan.backupData, plan.backupInfo},
		{plan.targetPath, plan.targetData, plan.targetInfo},
	} {
		data, info, err := readRestoreFile(file.path)
		if err != nil {
			return err
		}
		if !os.SameFile(info, file.info) || !bytes.Equal(data, file.data) {
			return errors.New("恢复预览后备份或目标已经变化，请重新选择并确认；未执行恢复")
		}
	}
	return nil
}

func applyYAMLRestore(plan *yamlRestorePlan) (string, string, error) {
	if err := checkYAMLRestorePlan(plan); err != nil {
		return "", "", err
	}
	guard, err := openYAMLGuard(plan.targetPath)
	if err != nil {
		return "", "", err
	}
	defer guard.Close()
	if err = checkYAMLRestorePlan(plan); err != nil {
		return "", "", err
	}
	safety, err := backupFile(plan.targetPath)
	if err != nil {
		return "", "", err
	}
	if err = checkYAMLRestorePlan(plan); err != nil {
		return "", safety, err
	}
	saved, err := os.ReadFile(safety)
	if err != nil || !bytes.Equal(saved, plan.targetData) {
		return "", safety, errors.New("保护备份与确认快照不一致，已停止恢复")
	}
	guard.closeSource()
	if err = checkYAMLRestorePlan(plan); err != nil {
		return "", safety, err
	}
	if err = replace(plan.targetPath, plan.backupData); err != nil {
		return "", safety, fmt.Errorf("当前配置已备份到 %s，但恢复写入失败：%w", safety, err)
	}
	return plan.targetPath, safety, nil
}

func checkFileSnapshot(path string, info os.FileInfo, data []byte) error {
	current, currentInfo, err := readRestoreFile(path)
	if err != nil {
		return err
	}
	if !os.SameFile(info, currentInfo) || !bytes.Equal(data, current) {
		return errors.New("配置文件已经变化，请重新选择并确认；未写入配置")
	}
	return nil
}

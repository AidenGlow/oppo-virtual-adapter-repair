//go:build windows

package main

import "strings"

var networkUndoNotice = alertFor

var confirmNetworkUndo = func(owner uintptr, plan *yamlNetworkUndoPlan) bool {
	return alertFor(owner, "最终确认：只撤销网卡设置", "配置文件：\n"+plan.restore.targetPath+"\n\n原修复备份：\n"+plan.restore.backupPath+"\n\n仅撤销以下字段：\n"+plan.summary+"\n\n保留当前节点、规则和其他设置；撤销前另存当前配置保护备份。\n点击‘是’才会修改。", yesNo|warning)
}

func undoChosenNetworkBackup(owner uintptr, backup string) bool {
	if backup == "" {
		return false
	}
	plan, err := prepareNetworkUndo(backup)
	if err != nil {
		networkUndoNotice(owner, "无法只撤销网卡设置", fileFailureMessage(err, modeYAML, backup), errIcon)
		return false
	}
	if backupTarget != "" && !sameConfigPath(backupTarget, plan.restore.targetPath) {
		networkUndoNotice(owner, "备份不对应当前配置", "请选择当前配置对应的原修复备份。", warning)
		return false
	}
	candidate := pathCandidate{Path: plan.restore.targetPath, Client: clientManual}
	if selectedProxy != nil && sameConfigPath(selectedProxy.Path, candidate.Path) {
		candidate = *selectedProxy
	}
	if err = verifyProxyTarget(&candidate); err != nil {
		networkUndoNotice(owner, "请先准备客户端", err.Error(), warning)
		return false
	}
	if !confirmNetworkUndo(owner, plan) {
		return false
	}
	if err = verifyProxyTarget(&candidate); err != nil {
		networkUndoNotice(owner, "配置已经变化", err.Error(), warning)
		return false
	}
	stopEffectivenessCheck()
	dest, protect, err := applyNetworkUndo(plan)
	if err != nil {
		detail := fileFailureMessage(err, modeYAML, plan.restore.targetPath)
		if protect != "" {
			detail += "\n当前配置保护备份：\n" + protect
		}
		text(statusText, detail)
		networkUndoNotice(owner, "撤销未完成", detail, errIcon)
		return false
	}
	delete(repairExpectations, strings.ToLower(dest))
	text(summaryText, "仅撤销网卡设置；节点与规则保留当前内容。")
	text(statusText, "已撤销网卡设置："+dest+"\r\n当前配置保护备份："+protect)
	refreshMainActions()
	networkUndoNotice(owner, "撤销完成", "已保留当前节点、规则和其他设置。\n请重新打开代理客户端，使撤销生效。", info)
	return true
}

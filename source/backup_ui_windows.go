//go:build windows

package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const (
	backupListID    = 801
	backupConfirmID = 802
	backupManualID  = 803
	backupRefreshID = 804
	backupCancelID  = 805
)

var (
	backupWindow, backupList, backupDetailsText, backupStatus, backupConfirm uintptr
	backupCallback                                                           uintptr
	backupView                                                               uiView
	backupTarget                                                             string
	backupEntries                                                            []YAMLBackup
	chooseYAMLBackupFile                                                     = func(owner uintptr) string {
		return filePick(owner, "手动选择 YAML 备份", filts("本工具生成的备份 (*.bak)", "*.bak", "所有文件 (*.*)", "*.*"))
	}
	confirmYAMLRestore = func(owner uintptr, plan *yamlRestorePlan) bool {
		return alertFor(owner, "最终确认：恢复 YAML", "恢复来源：\n"+plan.backupPath+"\n\n即将覆盖的 YAML：\n"+plan.targetPath+"\n\n请核对两个完整路径。恢复前会备份当前文件。\n点击‘是’才会执行；‘否’或关闭窗口不修改。", yesNo|warning)
	}
)

func backupUI(h uintptr) {
	backupView.init(h)
	addChrome(&backupView, "选择 YAML 备份", false)
	a := func(class, title string, x, y, w, height int32, style uint32, id uintptr, role int, key string) uintptr {
		return backupView.add(class, title, x, y, w, height, style, id, role, modeAny, key)
	}
	a("STATIC", "当前配置的备份 · 按备份时间从新到旧排列", 24, 56, -24, 22, 0, 0, 1, "")
	target := backupTarget
	if target == "" {
		target = "尚未选择配置，可返回主界面选择，或点击下方“手动选择文件”。"
	}
	a("EDIT", target, 24, 84, -24, 44, esReadonly|esMultiline|esAutoVScroll|wTab, 0, 1, "")
	backupList = a("LISTBOX", "", 24, 140, -24, 100, wBorder|wScroll|wTab|lbNotify|0x10|0x40|0x100, backupListID, 0, "list")
	backupDetailsText = a("EDIT", "尚未选择备份。", 24, 0, -24, 64, esReadonly|esMultiline|esAutoVScroll|wScroll|wTab, 0, 1, "details")
	backupStatus = a("STATIC", "", 24, 0, -24, 36, 0, 0, 1, "status")
	a("BUTTON", "手动选择文件…", 24, 0, 144, 32, bsOwnerDraw|wTab, backupManualID, 0, "manual")
	a("BUTTON", "刷新列表", 180, 0, 88, 32, bsOwnerDraw|wTab, backupRefreshID, 0, "refresh")
	backupConfirm = a("BUTTON", "选定并进入最终确认", -312, 0, 180, 32, bsOwnerDraw|wTab, backupConfirmID, 0, "confirm")
	a("BUTTON", "取消", -120, 0, 96, 32, bsOwnerDraw|wTab, backupCancelID, 0, "cancel")
	enable(backupConfirm, false)
	layoutBackupChooser()
	setupFrame(h)
}

func layoutBackupChooser() {
	if backupView.hwnd == 0 {
		return
	}
	w, h := backupView.size()
	listHeight := h - 318
	if listHeight < 22 {
		listHeight = 22
	}
	for _, c := range backupView.controls {
		x, y, width, height := c.x, c.y, c.w, c.h
		if x < 0 {
			x += w
		}
		if width < 0 {
			width += w - x
		}
		switch c.key {
		case "list":
			height = listHeight
		case "details":
			y = 150 + listHeight
		case "status":
			y = h - 96
		case "manual", "refresh", "confirm", "cancel":
			y = h - 48
		}
		backupView.place(c, x, y, width, height)
	}
	invalidateRectProc.Call(backupWindow, 0, 1)
}

func refreshYAMLBackups() {
	backupEntries = nil
	sendMessage.Call(backupList, listReset, 0, 0)
	text(backupDetailsText, "尚未选择备份。选择后仍需最终确认，才会恢复。")
	enable(backupConfirm, false)
	if backupTarget == "" {
		text(backupStatus, "未选择当前配置；可先返回主界面选择，或手动选择备份文件。")
		return
	}
	entries, err := findYAMLBackups(backupTarget)
	if err != nil {
		text(backupStatus, "未能读取当前配置的备份，可手动选择文件。")
		text(backupDetailsText, err.Error())
		return
	}
	backupEntries = entries
	for _, entry := range entries {
		label := pstr(entry.Time.Format("2006-01-02 15:04:05.000") + " | " + entry.Path)
		sendMessage.Call(backupList, listAdd, 0, uintptr(unsafe.Pointer(label)))
		runtime.KeepAlive(label)
	}
	if len(entries) == 0 {
		text(backupStatus, "当前配置未找到对应备份；可手动选择文件，未修改任何文件。")
	} else {
		text(backupStatus, fmt.Sprintf("找到 %d 份备份，请选择；恢复前请完全退出代理客户端。", len(entries)))
	}
}

func selectedYAMLBackup() int {
	r, _, _ := sendMessage.Call(backupList, listSelected, 0, 0)
	if int32(r) < 0 || int(r) >= len(backupEntries) {
		return -1
	}
	return int(r)
}

func showYAMLBackupDetails() {
	i := selectedYAMLBackup()
	enable(backupConfirm, i >= 0)
	if i < 0 {
		text(backupDetailsText, "尚未选择备份。")
		return
	}
	entry := backupEntries[i]
	text(backupDetailsText, "备份时间："+entry.Time.Format("2006-01-02 15:04:05.000")+"\r\n"+entry.Path)
}

// 自动列表及手动文件都使用同一只读校验、最终确认和带保护备份的提交入口。
func restoreChosenYAMLBackup(owner uintptr, backup string) bool {
	if backup == "" {
		return false
	}
	plan, err := prepareYAMLRestore(backup)
	if err != nil {
		alertFor(owner, "备份校验失败", err.Error(), errIcon)
		return false
	}
	checkClient := func() error {
		if selectedProxy != nil && strings.EqualFold(filepath.Clean(selectedProxy.Path), filepath.Clean(plan.targetPath)) {
			return verifyProxyTarget(selectedProxy)
		}
		return nil
	}
	if err = checkClient(); err != nil {
		alertFor(owner, "请先准备客户端", err.Error(), warning)
		return false
	}
	if !confirmYAMLRestore(owner, plan) {
		return false
	}
	if err = checkClient(); err != nil {
		alertFor(owner, "配置已经变化", err.Error(), warning)
		return false
	}
	dest, protect, err := applyYAMLRestore(plan)
	if err != nil {
		detail := err.Error()
		if protect != "" {
			detail += "\n\n当前配置保护备份：\n" + protect
		}
		alertFor(owner, "恢复失败", detail, errIcon)
		return false
	}
	useManualProxy(dest)
	refreshMainActions()
	text(statusText, "已恢复："+dest+"\r\n当前版本保护备份："+protect)
	alertFor(owner, "恢复完成", "请重新打开代理客户端，使恢复后的配置生效。", info)
	return true
}

func backupCommand(id uintptr) {
	switch id {
	case backupRefreshID:
		refreshYAMLBackups()
	case backupListID:
		showYAMLBackupDetails()
	case backupConfirmID:
		if i := selectedYAMLBackup(); i >= 0 && restoreChosenYAMLBackup(backupWindow, backupEntries[i].Path) {
			destroyWindow.Call(backupWindow)
		}
	case backupManualID:
		if restoreChosenYAMLBackup(backupWindow, chooseYAMLBackupFile(backupWindow)) {
			destroyWindow.Call(backupWindow)
		}
	case backupCancelID, 2:
		destroyWindow.Call(backupWindow)
	}
}

func drawYAMLBackupItem(l unsafe.Pointer) uintptr {
	d := (*drawItemData)(l)
	fill := uintptr(colorWhite)
	if d.State&1 != 0 {
		fill = colorSelected
	}
	fillRect(d.DC, d.Rect, fill)
	if int32(d.ItemID) < 0 || int(d.ItemID) >= len(backupEntries) {
		return 1
	}
	entry, dpi := backupEntries[d.ItemID], backupView.dpi
	r := d.Rect
	r.Left += scale(12, dpi)
	r.Right -= scale(12, dpi)
	r.Top += scale(3, dpi)
	r.Bottom = r.Top + scale(22, dpi)
	title := entry.Time.Format("2006-01-02 15:04:05.000")
	if entry.Sequence > 0 {
		title += fmt.Sprintf(" · 序号 %d", entry.Sequence)
	}
	drawLabel(d.DC, title, r, colorInk, backupView.fonts[0], 0x8024)
	r.Top += scale(23, dpi)
	r.Bottom = d.Rect.Bottom - scale(2, dpi)
	drawLabel(d.DC, filepath.Base(entry.Path), r, colorMuted, backupView.fonts[1], 0x8024)
	if d.State&0x10 != 0 {
		u32.NewProc("DrawFocusRect").Call(d.DC, uintptr(unsafe.Pointer(&d.Rect)))
	}
	return 1
}

func backupProc(h uintptr, m uint32, w uintptr, l unsafe.Pointer) uintptr {
	if r, handled := frameMessage(&backupView, h, m, w, l); handled {
		return r
	}
	switch m {
	case wmCreate:
		backupWindow = h
		backupUI(h)
		return 0
	case wmCommand:
		if !chromeCommand(h, w&0xffff) {
			backupCommand(w & 0xffff)
		}
		return 0
	case wmSize:
		layoutBackupChooser()
		return 0
	case wmPaint:
		paintViewChrome(&backupView, h)
		return 0
	case wmDPIChanged:
		handleDPI(&backupView, w, l)
		layoutBackupChooser()
		return 0
	case wmDrawItem:
		if (*drawItemData)(l).Type == 2 {
			return drawYAMLBackupItem(l)
		}
		return drawButton(l)
	case wmMeasureItem:
		if d := (*measureItemData)(l); d.ID == backupListID {
			dpi := backupView.dpi
			if dpi <= 0 {
				dpi = 96
			}
			d.Height = uint32(scale(48, dpi))
			return 1
		}
	case wmGetMinMaxInfo:
		minimumWindowSize(h, l, true)
		return 0
	case wmCtlColorStatic, wmCtlColorEdit, wmCtlColorButton, wmCtlColorList:
		return controlColors(w, uintptr(l))
	case wmDestroy:
		backupView.close()
		backupWindow, backupList, backupDetailsText, backupStatus, backupConfirm = 0, 0, 0, 0, 0
		backupEntries = nil
		backupTarget = ""
		return 0
	}
	r, _, _ := defaultProc.Call(h, uintptr(m), w, uintptr(l))
	return r
}

func createYAMLBackupWindow(target string) uintptr {
	if backupCallback == 0 {
		backupCallback = syscall.NewCallback(backupProc)
	}
	backupTarget = target
	registerNativeClass("OppoRepairBackupWindow2026", backupCallback, 6)
	h := createNativeWindow("OppoRepairBackupWindow2026", "选择 YAML 备份 · "+appVersion, win, 700, 500, wCaption|wSysMenu|wMaximize|wClipChildren|0x00040000)
	refreshYAMLBackups()
	return h
}

func launchYAMLBackupChooser() {
	if backupWindow != 0 {
		return
	}
	h := createYAMLBackupWindow(yamlPath)
	enable(win, false)
	defer func() {
		if win != 0 {
			enable(win, true)
			foreground.Call(win)
		}
	}()
	showWindow.Call(h, 10)
	updateWindow.Call(h)
	var ev message
	for backupWindow != 0 {
		r, _, err := getMessage.Call(uintptr(unsafe.Pointer(&ev)), 0, 0, 0)
		if int32(r) <= 0 {
			if int32(r) < 0 {
				alertFor(h, "消息处理失败", err.Error(), errIcon)
			}
			if backupWindow != 0 {
				destroyWindow.Call(backupWindow)
			}
			if r == 0 {
				postQuit.Call(ev.WParam)
			}
			break
		}
		if handled, _, _ := isDialogMessageProc.Call(h, uintptr(unsafe.Pointer(&ev))); handled == 0 {
			translateMessage.Call(uintptr(unsafe.Pointer(&ev)))
			dispatchMessage.Call(uintptr(unsafe.Pointer(&ev)))
		}
	}
}

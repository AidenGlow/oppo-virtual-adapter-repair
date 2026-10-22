//go:build windows

package main

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var (
	u32              = syscall.NewLazyDLL("user32.dll")
	g32              = syscall.NewLazyDLL("gdi32.dll")
	k32              = syscall.NewLazyDLL("kernel32.dll")
	cd32             = syscall.NewLazyDLL("comdlg32.dll")
	regClass         = u32.NewProc("RegisterClassExW")
	createWindow     = u32.NewProc("CreateWindowExW")
	defaultProc      = u32.NewProc("DefWindowProcW")
	showWindow       = u32.NewProc("ShowWindow")
	updateWindow     = u32.NewProc("UpdateWindow")
	getMessage       = u32.NewProc("GetMessageW")
	translateMessage = u32.NewProc("TranslateMessage")
	dispatchMessage  = u32.NewProc("DispatchMessageW")
	setTextProc      = u32.NewProc("SetWindowTextW")
	sendMessage      = u32.NewProc("SendMessageW")
	messageBox       = u32.NewProc("MessageBoxW")
	postQuit         = u32.NewProc("PostQuitMessage")
	loadCursor       = u32.NewProc("LoadCursorW")
	sysMetrics       = u32.NewProc("GetSystemMetrics")
	enableWindow     = u32.NewProc("EnableWindow")
	destroyWindow    = u32.NewProc("DestroyWindow")
	foreground       = u32.NewProc("SetForegroundWindow")
	getModule        = k32.NewProc("GetModuleHandleW")
	openFileProc     = cd32.NewProc("GetOpenFileNameW")
	createFont       = g32.NewProc("CreateFontW")
	deleteObject     = g32.NewProc("DeleteObject")
)

const (
	wChild         = 0x40000000
	wVisible       = 0x10000000
	wTab           = 0x00010000
	wBorder        = 0x00800000
	wScroll        = 0x00200000
	wCaption       = 0x00C00000
	wSysMenu       = 0x00080000
	wMinimize      = 0x00020000
	wMaximize      = 0x00010000
	wClipChildren  = 0x02000000
	wPopup         = 0x80000000
	esReadonly     = 0x0800
	esMultiline    = 0x0004
	esAutoVScroll  = 0x0040
	esAutoHScroll  = 0x0080
	bsAutoCheckbox = 3
	bsGroup        = 7
	bsPush         = 0
	lbNotify       = 1
	wmCreate       = 0x0001
	wmDestroy      = 0x0002
	wmCommand      = 0x0111
	wmFont         = 0x0030
	checkGet       = 0x00F0
	checked        = 1
	listAdd        = 0x0180
	listReset      = 0x0184
	listSelected   = 0x0188
	listChanged    = 1
	yesNo          = 0x4
	yes            = 6
	warning        = 0x30
	info           = 0x40
	errIcon        = 0x10
	ofnExplorer    = 0x00080000
	ofnFileExist   = 0x00001000
	ofnPathExist   = 0x00000800
	ofnNoChdir     = 0x00000008
	selectDll      = 101
	renameDll      = 102
	restoreDll     = 103
	selectYaml     = 201
	openChooser    = 202
	restoreYAML    = 204
	helpID         = 301
	chooseListID   = 401
	chooseRefresh  = 402
	chooseConfirm  = 403
	chooseCancel   = 404
	chooseShowAll  = 405
	checkEffective = 207
	undoNetwork    = 208
)

type classDef struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type point struct{ X, Y int32 }
type message struct {
	Hwnd           uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          point
	Private        uint32
}
type fileDlg struct {
	Size                 uint32
	Owner, Instance      uintptr
	Filter, CustomFilter *uint16
	MaxCustom            uint32
	FilterIndex          uint32
	File                 *uint16
	MaxFile              uint32
	TitleFile            *uint16
	MaxTitle             uint32
	Directory, Title     *uint16
	Flags                uint32
	FileOffset, FileExt  uint16
	DefExt               *uint16
	Data, Hook           uintptr
	Template             *uint16
	Reserved             uintptr
	ReservedDW, FlagsEx  uint32
}

var (
	win, dllText, yamlText, summaryText, checkBox, statusText uintptr
	chooser, nicList, nicInfo, nicStatus                      uintptr
	nicConfirmButton, nicRefreshButton, nicCancelButton       uintptr
	nicShowAllButton, checkEffectiveButton                    uintptr
	undoNetworkButton                                         uintptr
	allAdapters                                               []Adapter
	showAllAdapters                                           bool
	renameButton, restoreDLLButton, openChooserButton         uintptr
	mainCallback, chooserProc                                 uintptr
	backgroundBrush                                           uintptr
	mainView, chooserView                                     uiView
	currentMode                                               repairMode
	dllPath, yamlPath                                         string
	adapters                                                  []Adapter
	scanSequence                                              uintptr
	activeScan                                                *adapterScanJob
	adapterScanner                                            = scanWindowsAdaptersContext
	mainLoopRunning                                           bool
)

func pstr(s string) *uint16 { p, _ := syscall.UTF16PtrFromString(s); return p }

func mainUI() {
	mainView.init(win)
	addChrome(&mainView, appTitle, true)
	a := func(class, title string, x, y, w, h int32, style uint32, id uintptr, role int, mode repairMode, key string) uintptr {
		return mainView.add(class, title, x, y, w, h, style, id, role, mode, key)
	}
	a("BUTTON", "帮助", -188, 7, 60, 28, bsOwnerDraw|wTab, helpID, 1, modeAny, "chromeHelp")
	a("BUTTON", "OPPO DLL 修复", 20, 56, 0, 30, bsOwnerDraw|wTab, chooseDLLMode, 3, modeAny, "choiceDLL")
	a("BUTTON", "Clash / Mihomo 配置", 0, 56, 0, 30, bsOwnerDraw|wTab, chooseYAMLMode, 3, modeAny, "choiceYAML")
	a("STATIC", "选择一种方式开始", 32, 156, -32, 28, 0, 0, 3, modeNone, "")
	a("STATIC", "DLL 修复：处理 OPPO 互联程序文件。\r\n网卡配置：指定代理使用的联网网卡。", 32, 202, -32, 48, 0, 0, 0, modeNone, "")
	a("STATIC", "两种方式独立使用，最终确认后才会修改文件。", 32, 286, -32, 38, 0, 0, 1, modeNone, "")

	a("STATIC", "修复 OPPO 程序文件", 32, 116, -32, 25, 0, 0, 3, modeDLL, "")
	a("STATIC", "先登录并连接手机；修复与恢复后都需要重启电脑。", 32, 145, -32, 20, 0, 0, 1, modeDLL, "")
	a("STATIC", "OPPO 安装文件 · 请核对实际安装目录", 32, 172, -32, 20, 0, 0, 1, modeDLL, "")
	dllText = a("COMBOBOX", "", 32, 196, -220, 32, 3|0x10|0x200|0x00200000|wTab, dllChoiceID, 0, modeDLL, "dllChoice")
	dllFindButton = a("BUTTON", "重新查找", -212, 196, 82, 32, bsOwnerDraw|wTab, findDLL, 0, modeDLL, "")
	a("BUTTON", "手动选择", -122, 196, 90, 32, bsOwnerDraw|wTab, selectDll, 0, modeDLL, "")
	checkBox = a("BUTTON", "我已登录并连接手机，且已核对文件属于 OPPO 互联", 32, 236, -32, 32, bsAutoCheckbox|0x2000|wTab, checkDLLReady, 0, modeDLL, "")
	a("STATIC", "可能遇到：手机连接失败、无法连接新设备或更新后需重修。\r\n如遇问题可点击恢复；请保留已改名 DLL。", 44, 281, -44, 40, 0, 0, 1, modeDLL, "notice")
	renameButton = a("BUTTON", "修复 DLL", 32, 340, 180, 36, bsOwnerDraw|wTab, renameDll, 0, modeDLL, "")
	restoreDLLButton = a("BUTTON", "恢复 DLL", 224, 340, 128, 36, bsOwnerDraw|wTab, restoreDll, 0, modeDLL, "")

	a("STATIC", "指定 TUN 联网网卡", 32, 116, -32, 25, 0, 0, 3, modeYAML, "")
	a("STATIC", "支持修改软件", 32, 145, 96, 24, 0, 0, 1, modeYAML, "")
	a("STATIC", "", 136, 143, 24, 24, 0x000d, softwareRevIcon, 1, modeYAML, "")
	a("STATIC", "Clash Verge Rev", 168, 145, 160, 24, 0, 0, 1, modeYAML, "")
	a("STATIC", "", 340, 143, 24, 24, 0x000d, softwareCFWIcon, 1, modeYAML, "")
	a("STATIC", "Clash for Windows", 372, 145, -32, 24, 0, 0, 1, modeYAML, "")
	a("STATIC", "先完全退出代理软件；选择网卡并修复后重新打开。", 32, 172, -32, 20, 0, 0, 1, modeYAML, "")
	yamlText = a("COMBOBOX", "", 32, 196, -220, 32, 3|0x10|0x200|0x00200000|wTab, proxyChoiceID, 0, modeYAML, "proxyChoice")
	proxyFindButton = a("BUTTON", "重新识别", -212, 196, 82, 32, bsOwnerDraw|wTab, findProxy, 0, modeYAML, "")
	a("BUTTON", "手动指定", -122, 196, 90, 32, bsOwnerDraw|wTab, selectYaml, 0, modeYAML, "")
	summaryText = a("STATIC", "识别配置后，手动选择实际联网的 Wi-Fi 或以太网。", 32, 236, -32, 32, 0, 0, 1, modeYAML, "")
	a("STATIC", "可能遇到：选错网卡后断网，或订阅 / 脚本覆盖设置。\r\n如遇问题可点击恢复；请保留 .bak 备份及旁边的撤销记录。", 44, 281, -44, 40, 0, 0, 1, modeYAML, "notice")
	openChooserButton = a("BUTTON", "扫描并选择网卡", 32, 340, 132, 36, bsOwnerDraw|wTab, openChooser, 1, modeYAML, "yamlAction0")
	undoNetworkButton = a("BUTTON", "只撤销网卡设置", 172, 340, 132, 36, bsOwnerDraw|wTab, undoNetwork, 1, modeYAML, "yamlAction1")
	a("BUTTON", "恢复 YAML 备份", 312, 340, 132, 36, bsOwnerDraw|wTab, restoreYAML, 1, modeYAML, "yamlAction2")
	checkEffectiveButton = a("BUTTON", "检查是否生效", 452, 340, 132, 36, bsOwnerDraw|wTab, checkEffective, 1, modeYAML, "yamlAction3")
	a("STATIC", "执行结果", 32, 406, -32, 20, 0, 0, 1, modeAny, "resultTitle")
	statusText = a("EDIT", "就绪。请选择上方的一种修复方式。", 32, 431, -32, 40, esReadonly|esMultiline|esAutoVScroll|wScroll, 0, 0, modeAny, "result")
	a("STATIC", "非官方工具 · 仅在确认后修改所选文件", 16, 496, -16, 22, 0, 0, 1, modeAny, "footer")
	skinControl(dllText, skinCombo)
	skinControl(yamlText, skinCombo)
	skinControl(checkBox, skinCheckbox)
	refreshMainActions()
	fillPathChoices(dllText, "选择此方式后自动查找", nil)
	fillPathChoices(yamlText, "选择此方式后自动识别", nil)
	mainView.layoutMain()
	setupFrame(win)
}
func refreshMainActions() {
	ready, _, _ := sendMessage.Call(checkBox, checkGet, 0, 0)
	base := filepath.Base(dllPath)
	enable(renameButton, currentMode == modeDLL && strings.EqualFold(base, "wintun.dll") && ready == checked)
	enable(restoreDLLButton, currentMode == modeDLL && base == suffix)
	enable(openChooserButton, currentMode == modeYAML && yamlPath != "")
	enable(checkEffectiveButton, currentMode == modeYAML && yamlPath != "" && activeEffectivenessCheck == nil)
	enable(undoNetworkButton, currentMode == modeYAML && yamlPath != "")
}
func setRepairMode(mode repairMode) {
	if mode != modeDLL && mode != modeYAML {
		return
	}
	if currentMode != mode {
		stopEffectivenessCheck()
		stopPathSearch()
		sendMessage.Call(checkBox, 0x00f1, 0, 0)
	}
	currentMode = mode
	mainView.layoutMain()
	for _, c := range mainView.controls {
		if c.key == "choiceDLL" || c.key == "choiceYAML" {
			invalidateRectProc.Call(c.hwnd, 0, 1)
		}
	}
	refreshMainActions()
	if !searchedPaths[mode] && ((mode == modeDLL && dllPath == "") || (mode == modeYAML && yamlPath == "")) {
		beginPathSearch(mode)
	}
}
func filts(kv ...string) []uint16 {
	arr := make([]uint16, 0, 128)
	for _, v := range kv {
		arr = append(arr, syscall.StringToUTF16(v)...)
	}
	return append(arr, 0)
}
func filePick(owner uintptr, title string, filter []uint16) string {
	storage := make([]uint16, 32768)
	titlePtr := pstr(title)
	x := fileDlg{Size: uint32(unsafe.Sizeof(fileDlg{})), Owner: owner, Filter: &filter[0], FilterIndex: 1, File: &storage[0], MaxFile: uint32(len(storage)), Title: titlePtr, Flags: ofnExplorer | ofnFileExist | ofnPathExist | ofnNoChdir}
	r, _, _ := openFileProc.Call(uintptr(unsafe.Pointer(&x)))
	runtime.KeepAlive(filter)
	runtime.KeepAlive(storage)
	runtime.KeepAlive(titlePtr)
	if r == 0 {
		return ""
	}
	return syscall.UTF16ToString(storage)
}
func afterDLL() {
	if alertFor(win, "需要重启 Windows", "操作成功。DLL 修复或恢复后需要重启电脑。\n\n是否现在重启？请先保存工作。\n点击‘否’可以稍后自行重启。", yesNo|warning) {
		if e := exec.Command("shutdown.exe", "/r", "/t", "0").Start(); e != nil {
			alert("重启失败", e.Error()+"\n请自行重启。", errIcon)
		}
	} else {
		text(statusText, "文件修改成功。请稍后手动重启 Windows，再验证连接。")
	}
}
func renameAction(restore bool) {
	if currentMode != modeDLL || dllPath == "" {
		return
	}
	if !restore {
		r, _, _ := sendMessage.Call(checkBox, checkGet, 0, 0)
		if r != checked {
			alert("请先确认", "请先登录 OPPO 互联、保持手机已连接，再勾选确认。", warning)
			return
		}
	}
	original, changed, e := dllPair(dllPath)
	if e != nil {
		alert("路径错误", e.Error(), errIcon)
		return
	}
	source, target := original, changed
	title := "最终确认：修复 DLL"
	if restore {
		source, target = changed, original
		title = "最终确认：恢复 DLL"
	}
	body := "即将执行：\n" + source + "\n→\n" + target + "\n\n请确认这是 OPPO 互联的真实 daemon 安装目录。"
	if !restore {
		body += "\n\n必须已登录且手机保持连接；修改后可能不能连接新设备。"
	}
	body += "\n\n完成后需要重启 Windows。点击‘是’才会修改。"
	if !alertFor(win, title, body, yesNo|warning) {
		return
	}
	result, e := changeDLL(dllPath, restore)
	if e != nil {
		detail := fileFailureMessage(e, modeDLL, source)
		alert("操作失败", detail, errIcon)
		text(statusText, detail)
		return
	}
	useManualDLL(result)
	refreshMainActions()
	text(statusText, "DLL 操作成功："+result+"\r\n请重启 Windows 后验证。")
	afterDLL()
}
func restoreAction() {
	if currentMode != modeYAML {
		return
	}
	launchYAMLBackupChooser()
}
func mainCommand(id uintptr) {
	if chromeCommand(win, id) {
		return
	}
	if id == chooseDLLMode {
		setRepairMode(modeDLL)
		return
	}
	if id == chooseYAMLMode {
		setRepairMode(modeYAML)
		return
	}
	if (id >= selectDll && id <= dllChoiceID && currentMode != modeDLL) || (id >= selectYaml && id <= findProxy && currentMode != modeYAML) {
		return
	}
	switch id {
	case selectDll:
		p := filePick(win, "选择 OPPO daemon 内的 DLL 或改名文件", filts("DLL 及其改名文件", "wintun.dll*", "全部文件", "*.*"))
		if p == "" {
			return
		}
		if _, _, e := dllPair(p); e != nil {
			alert("路径错误", e.Error(), errIcon)
			return
		}
		useManualDLL(p)
		sendMessage.Call(checkBox, 0x00f1, 0, 0)
		refreshMainActions()
		text(statusText, "已选择文件。请核对真实安装目录及手机连接状态。")
	case checkDLLReady:
		refreshMainActions()
	case renameDll:
		renameAction(false)
	case restoreDll:
		renameAction(true)
	case selectYaml:
		p := filePick(win, "选择 Clash / Mihomo YAML 或 Merge 文件", filts("YAML 文件", "*.yaml;*.yml", "全部文件", "*.*"))
		if p == "" {
			return
		}
		if e := validateYAMLPath(p); e != nil {
			alert("格式错误", e.Error(), warning)
			return
		}
		useManualProxy(p)
		refreshMainActions()
	case openChooser:
		if yamlPath != "" {
			if err := verifyProxyTarget(selectedProxy); err != nil {
				alert("请先准备客户端", err.Error(), warning)
				return
			}
			launchChooser()
		}
	case findDLL:
		beginPathSearch(modeDLL)
	case findProxy:
		beginPathSearch(modeYAML)
	case restoreYAML:
		restoreAction()
	case checkEffective:
		beginEffectivenessCheck()
	case undoNetwork:
		if currentMode == modeYAML && yamlPath != "" {
			launchNetworkUndoChooser()
		}
	case helpID:
		alert("使用帮助", "两种修复方式独立使用，无需同时执行。\n\nDLL：选择方式后自动查找 OPPO 文件；多处结果时选择真实安装目录，找不到可手动选择。必须先登录 OPPO 互联并保持手机连接，勾选前提后修复。修复与恢复都需要重启电脑。\n\n网卡配置：选择 Clash Verge Rev 或 Clash for Windows，工具自动定位配置，不需要自行找 YAML。先完全退出所选代理软件，再扫描并手动选择联网网卡。最终确认后才备份修改，完成后重新打开客户端。CFW 须先在 Profiles 启用配置，订阅刷新可能重置修改。默认只显示已连接物理网卡，可点‘显示全部’；修复并重开客户端后可点‘检查是否生效’。\n\n撤销：‘只撤销网卡设置’保留当前节点和规则，需要原 .bak 及旁边的 .oppo-repair.json；‘恢复 YAML 备份’会还原整份配置。均需先退出客户端并最终确认。文件占用失败时会尝试显示相关程序和 PID，请保存工作后正常退出。\n\n不会修改系统 DNS、路由或网卡驱动。", info)
	}
}
func mainProc(h uintptr, m uint32, w uintptr, l unsafe.Pointer) uintptr {
	if r, handled := frameMessage(&mainView, h, m, w, l); handled {
		return r
	}
	switch m {
	case wmCreate:
		win = h
		mainUI()
		return 0
	case wmCommand:
		id, notify := w&0xffff, (w>>16)&0xffff
		if id == dllChoiceID || id == proxyChoiceID {
			if notify == 1 {
				if id == dllChoiceID {
					useDLLChoice()
				} else {
					useProxyChoice()
				}
			}
			return 0
		}
		mainCommand(id)
		return 0
	case wmEffectivenessDone:
		finishEffectivenessCheck(w)
		return 0
	case wmPathsDone:
		finishPathSearch(w)
		return 0
	case wmSize:
		mainView.layoutMain()
		return 0
	case wmPaint:
		paintMain(h)
		return 0
	case wmDrawItem:
		if (*drawItemData)(l).Type == 3 {
			return drawComboItem(l)
		}
		if (*drawItemData)(l).Type == 5 {
			return drawSoftwareIcon(l)
		}
		return drawButton(l)
	case wmMeasureItem:
		d := (*measureItemData)(l)
		if d.ID == dllChoiceID || d.ID == proxyChoiceID {
			dpi := mainView.dpi
			if dpi <= 0 {
				dpi = 96
			}
			d.Height = uint32(scale(32, dpi))
			return 1
		}
	case wmGetMinMaxInfo:
		minimumWindowSize(h, l, false)
		return 0
	case wmCtlColorStatic, wmCtlColorEdit, wmCtlColorButton:
		return controlColors(w, uintptr(l))
	case wmVScroll:
		switch w & 0xffff {
		case 0:
			mainView.scroll -= 28
		case 1:
			mainView.scroll += 28
		case 2:
			mainView.scroll -= 160
		case 3:
			mainView.scroll += 160
		case 4, 5:
			s := scrollData{Size: uint32(unsafe.Sizeof(scrollData{})), Mask: 0x10}
			getScrollInfoProc.Call(h, 1, uintptr(unsafe.Pointer(&s)))
			mainView.scroll = s.Track
		case 6:
			mainView.scroll = 0
		case 7:
			mainView.scroll = contentHeight
		}
		mainView.layoutMain()
		return 0
	case wmMouseWheel:
		mainView.scroll -= int32(int16(w>>16)) / 120 * 42
		mainView.layoutMain()
		return 0
	case wmDPIChanged:
		handleDPI(&mainView, w, l)
		mainView.layoutMain()
		return 0
	case wmDestroy:
		stopEffectivenessCheck()
		stopPathSearch()
		mainView.close()
		win = 0
		if mainLoopRunning {
			postQuit.Call(0)
		}
		return 0
	}
	r, _, _ := defaultProc.Call(h, uintptr(m), w, uintptr(l))
	return r
}
func nicUI() {
	chooserView.init(chooser)
	addChrome(&chooserView, "选择联网网卡", false)
	a := func(class, title string, x, y, w, h int32, style uint32, id uintptr, role int, key string) uintptr {
		return chooserView.add(class, title, x, y, w, h, style, id, role, modeAny, key)
	}
	a("STATIC", "优先显示已连接物理网卡，仍需手动选择。", 24, 56, -176, 24, 0, 0, 1, "")
	nicShowAllButton = a("BUTTON", "显示全部", -164, 52, 140, 32, bsOwnerDraw|wTab, chooseShowAll, 0, "")
	showAllAdapters = false
	allAdapters = nil
	nicList = a("LISTBOX", "", 24, 92, -24, 100, wBorder|wScroll|wTab|lbNotify|0x10|0x40|0x100, chooseListID, 0, "list")
	nicInfo = a("EDIT", "尚未选择网卡。", 24, 0, -24, 88, esReadonly|esMultiline|wBorder|wScroll|wTab, 0, 0, "details")
	a("STATIC", "通常选 Wi-Fi / 以太网硬件网卡；默认路由仅作参考。", 24, 0, -24, 23, 0, 0, 1, "hint")
	nicStatus = a("STATIC", "等待扫描…", 24, 0, -24, 25, 0, 0, 1, "status")
	nicRefreshButton = a("BUTTON", "重新扫描", 24, 0, 122, 34, bsOwnerDraw|wTab, chooseRefresh, 0, "refresh")
	nicConfirmButton = a("BUTTON", "选定并进入最终确认", 0, 0, 192, 34, bsOwnerDraw|wTab, chooseConfirm, 0, "confirm")
	nicCancelButton = a("BUTTON", "取消", 0, 0, 108, 34, bsOwnerDraw|wTab, chooseCancel, 0, "cancel")
	enable(nicConfirmButton, false)
	chooserView.layoutChooser()
	setupFrame(chooser)
}
func selected() int {
	if nicList == 0 || activeScan != nil {
		return -1
	}
	r, _, _ := sendMessage.Call(nicList, listSelected, 0, 0)
	if int32(r) == -1 || int(r) >= len(adapters) {
		return -1
	}
	return int(r)
}
func nicDetails() {
	i := selected()
	enable(nicConfirmButton, i >= 0)
	if i < 0 {
		text(nicInfo, "尚未选择网卡。")
		return
	}
	a := adapters[i]
	routeInfo := "未发现 IPv4 默认路由"
	if !a.RouteKnown {
		routeInfo = "默认路由未能读取"
	} else if a.HasRoute {
		routeInfo = "存在 IPv4 默认路由（仅参考）"
	}
	text(nicInfo, fmt.Sprintf("接口名称：%s  |  索引：%d\r\n类型：%s  |  状态：%s\r\n设备：%s\r\nMAC：%s  |  %s；网关：%s\r\n%s", a.Name, a.Index, adapterKind(a), adapterState(a), a.Description, a.MAC, routeInfo, a.Gateway, adapterRecommendation(a)))
}
func nicScan() {
	if activeScan != nil {
		activeScan.cancel()
	}
	scanSequence++
	token, owner := scanSequence, chooser
	adapters = nil
	allAdapters = nil
	enable(nicShowAllButton, false)
	sendMessage.Call(nicList, listReset, 0, 0)
	text(nicInfo, "扫描期间可随时取消。")
	text(nicStatus, "正在读取 Windows 网卡…")
	enable(nicConfirmButton, false)
	enable(nicRefreshButton, false)
	activeScan = startAdapterScan(token, adapterScanner, func() { postMessageProc.Call(owner, wmScanDone, token, 0) })
}
func finishNICScan(token uintptr) {
	if chooser == 0 || activeScan == nil || activeScan.token != token {
		return
	}
	select {
	case result := <-activeScan.result:
		activeScan.cancel()
		activeScan = nil
		enable(nicRefreshButton, true)
		if result.err != nil {
			text(nicInfo, "尚未选择网卡。")
			enable(nicShowAllButton, true)
			text(nicStatus, "扫描失败，可重试；未修改文件。")
			alertFor(chooser, "网卡扫描失败", result.err.Error(), errIcon)
			return
		}
		allAdapters = markRecommendedAdapters(result.adapters)
		enable(nicShowAllButton, true)
		fillNICList()

	default:
	}
}
func fillNICList() {
	adapters = visibleAdapters(allAdapters, showAllAdapters)
	sendMessage.Call(nicList, listReset, 0, 0)
	for _, a := range adapters {
		extra := ""
		if a.Recommended {
			extra = " · 建议选择"
		} else if a.HasRoute {
			extra = " · 默认路由"
		}
		row := pstr(fmt.Sprintf("%s  |  %s  |  %s%s", a.Name, adapterKind(a), adapterState(a), extra))
		sendMessage.Call(nicList, listAdd, 0, uintptr(unsafe.Pointer(row)))
		runtime.KeepAlive(row)
	}
	text(nicInfo, "尚未选择网卡。推荐仅供参考，不会自动选中或修改。")
	enable(nicConfirmButton, false)
	label := "显示全部"
	if showAllAdapters {
		label = "仅显示联网物理网卡"
	}
	text(nicShowAllButton, label)
	status := fmt.Sprintf("显示 %d / %d 张网卡，请手动选择。", len(adapters), len(allAdapters))
	if len(adapters) == 0 && !showAllAdapters {
		status = "未找到已连接物理网卡，可点击‘显示全部’查看。"
	}
	text(nicStatus, status)
}
func nicConfirm() {
	i := selected()
	if i < 0 {
		return
	}
	if err := verifyProxyTarget(selectedProxy); err != nil {
		alertFor(chooser, "请重新识别客户端", err.Error(), warning)
		return
	}
	a := adapters[i]
	hint := ""
	if !a.HardwareKnown {
		hint += "\n【注意】硬件属性未知，请在 Windows 网络设置中核对。\n"
	} else if !a.Hardware {
		hint += "\n【注意】这是非硬件网卡，请核对是否为代理 TUN / TAP。\n"
	}
	if !strings.EqualFold(a.Status, "Up") {
		hint += "\n【注意】当前未显示已连接。\n"
	}
	body := fmt.Sprintf("配置文件：\n%s\n\n所选网卡：%s\n%s\n类型：%s；状态：%s%s\n\n改动：\ninterface-name: %q（顶层）\ntun.auto-detect-interface: false\n\n点击‘是’才会先备份再修改。\n点击‘否’不修改。", yamlPath, a.Name, a.Description, adapterKind(a), adapterState(a), hint, a.Name)
	if !alertFor(chooser, "最终确认：修复配置", body, yesNo|warning) {
		return
	}
	if err := verifyProxyTarget(selectedProxy); err != nil {
		alertFor(chooser, "配置已经变化", err.Error(), warning)
		return
	}
	backup, e := updateYaml(yamlPath, a.Name)
	if e != nil {
		detail := fileFailureMessage(e, modeYAML, yamlPath)
		if backup != "" {
			detail += "\n\n修改前备份：\n" + backup
		}
		alertFor(chooser, "操作未完成", detail, errIcon)
		text(statusText, "YAML 修复未完成："+detail)
		return
	}
	rememberRepairExpectation(yamlPath, a.Name)
	text(summaryText, "上次选择："+a.Name+"。再次修改仍需重新选择并确认。")
	text(statusText, "YAML 已修改；网卡："+a.Name+"\r\n修改前备份："+backup)
	alertFor(chooser, "修改完成", "已备份到：\n"+backup+"\n\n请重新打开所选代理客户端，使修改生效，再点击‘检查是否生效’。如需只撤销网卡改动，请保留原备份及旁边的 .oppo-repair.json 记录。CFW 订阅刷新可能覆盖修改。", info)
	destroyWindow.Call(chooser)
}
func chooserProcFn(h uintptr, m uint32, w uintptr, l unsafe.Pointer) uintptr {
	if r, handled := frameMessage(&chooserView, h, m, w, l); handled {
		return r
	}
	switch m {
	case wmCreate:
		chooser = h
		nicUI()
		return 0
	case wmCommand:
		if chromeCommand(h, w&0xffff) {
			return 0
		}
		switch w & 0xffff {
		case chooseListID:
			if (w>>16)&0xffff == listChanged {
				nicDetails()
			}
		case chooseShowAll:
			if activeScan == nil {
				showAllAdapters = !showAllAdapters
				fillNICList()
			}
		case chooseRefresh:
			if activeScan == nil {
				nicScan()
			}
		case chooseConfirm:
			nicConfirm()
		case chooseCancel, 2:
			destroyWindow.Call(h)
		}
		return 0
	case wmScanDone:
		finishNICScan(w)
		return 0
	case wmSize:
		chooserView.layoutChooser()
		return 0
	case wmPaint:
		paintChooserChrome(h)
		return 0
	case wmDPIChanged:
		handleDPI(&chooserView, w, l)
		chooserView.layoutChooser()
		return 0
	case wmDrawItem:
		if (*drawItemData)(l).Type == 2 {
			return drawNICItem(l)
		}
		return drawButton(l)
	case wmMeasureItem:
		d := (*measureItemData)(l)
		if d.ID == chooseListID {
			d.Height = uint32(scale(48, chooserView.dpi))
			return 1
		}
	case wmGetMinMaxInfo:
		minimumWindowSize(h, l, true)
		return 0
	case wmCtlColorStatic, wmCtlColorEdit, wmCtlColorButton, wmCtlColorList:
		return controlColors(w, uintptr(l))
	case wmDestroy:
		if activeScan != nil {
			activeScan.cancel()
			activeScan = nil
		}
		chooserView.close()
		chooser = 0
		nicList = 0
		nicInfo = 0
		nicStatus = 0
		adapters = nil
		nicConfirmButton = 0
		nicRefreshButton = 0
		nicCancelButton = 0
		nicShowAllButton = 0
		allAdapters = nil
		return 0
	}
	r, _, _ := defaultProc.Call(h, uintptr(m), w, uintptr(l))
	return r
}
func createChooserWindow() uintptr {
	if chooserProc == 0 {
		chooserProc = syscall.NewCallback(chooserProcFn)
	}
	registerNativeClass("OppoRepairChooseWindow2026", chooserProc, 6)
	return createNativeWindow("OppoRepairChooseWindow2026", "选择联网网卡 · "+appVersion, win, 700, 552, wCaption|wSysMenu|wMaximize|wClipChildren|0x00040000)
}
func launchChooser() {
	if chooser != 0 {
		return
	}
	h := createChooserWindow()
	enable(win, false)
	defer func() {
		if win != 0 {
			enable(win, true)
			foreground.Call(win)
		}
	}()
	showWindow.Call(h, 10)
	updateWindow.Call(h)
	nicScan()
	var ev message
	for chooser != 0 {
		r, _, e := getMessage.Call(uintptr(unsafe.Pointer(&ev)), 0, 0, 0)
		if int32(r) == -1 {
			alertFor(chooser, "消息处理失败", e.Error(), errIcon)
			if chooser != 0 {
				destroyWindow.Call(chooser)
			}
			break
		}
		if r == 0 {
			if chooser != 0 {
				destroyWindow.Call(chooser)
			}
			postQuit.Call(ev.WParam)
			break
		}
		if handled, _, _ := isDialogMessageProc.Call(chooser, uintptr(unsafe.Pointer(&ev))); handled == 0 {
			translateMessage.Call(uintptr(unsafe.Pointer(&ev)))
			dispatchMessage.Call(uintptr(unsafe.Pointer(&ev)))
		}
	}
}
func createMainWindow() uintptr {
	if backgroundBrush == 0 {
		backgroundBrush, _, _ = g32.NewProc("CreateSolidBrush").Call(colorBackground)
	}
	if mainCallback == 0 {
		mainCallback = syscall.NewCallback(mainProc)
	}
	registerNativeClass("OPPOConnectRepairNative2026", mainCallback, backgroundBrush)
	currentMode = modeNone
	dllPath = ""
	yamlPath = ""
	searchedPaths = [3]bool{}
	selectedProxy = nil
	dllCandidates = nil
	proxyCandidates = nil
	return createNativeWindow("OPPOConnectRepairNative2026", appTitle+" "+appVersion, 0, 680, 524, wCaption|wSysMenu|wMinimize|wMaximize|wClipChildren|0x00040000|0x00200000)
}
func main()                     { runOnGUIThread(runGUI) }
func runOnGUIThread(run func()) { runtime.LockOSThread(); defer runtime.UnlockOSThread(); run() }
func runGUI() {
	configureDPIAwareness()
	mainLoopRunning = true
	defer func() { mainLoopRunning = false }()
	h := createMainWindow()
	showWindow.Call(h, 10)
	updateWindow.Call(h)
	var ev message
	for {
		r, _, e := getMessage.Call(uintptr(unsafe.Pointer(&ev)), 0, 0, 0)
		if int32(r) == -1 {
			alert("消息处理失败", e.Error(), errIcon)
			destroyWindow.Call(h)
			return
		}
		if r == 0 {
			return
		}
		if handled, _, _ := isDialogMessageProc.Call(h, uintptr(unsafe.Pointer(&ev))); handled == 0 {
			translateMessage.Call(uintptr(unsafe.Pointer(&ev)))
			dispatchMessage.Call(uintptr(unsafe.Pointer(&ev)))
		}
	}
}

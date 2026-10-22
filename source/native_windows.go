//go:build windows

package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

const (
	wmSize                      = 0x0005
	wmPaint                     = 0x000f
	wmDrawItem                  = 0x002b
	wmMeasureItem               = 0x002c
	wmGetMinMaxInfo             = 0x0024
	wmCtlColorButton            = 0x0135
	wmCtlColorEdit              = 0x0133
	wmCtlColorList              = 0x0134
	wmCtlColorStatic            = 0x0138
	wmVScroll                   = 0x0115
	wmMouseWheel                = 0x020a
	wmDPIChanged                = 0x02e0
	wmScanDone                  = 0x8002
	bsOwnerDraw                 = 0x000b
	modeNone         repairMode = 0
	modeDLL          repairMode = 1
	modeYAML         repairMode = 2
	modeAny          repairMode = -1
	chooseDLLMode               = 501
	chooseYAMLMode              = 502
	checkDLLReady               = 104
	contentHeight    int32      = 524
	mainPanelTop     int32      = 102
	mainPanelBottom  int32      = 388
	colorBackground             = 0x00f9f7f5
	colorWhite                  = 0x00ffffff
	colorInk                    = 0x00262b17
	colorMuted                  = 0x005f6658
	colorBlue                   = 0x0060800b
	colorBorder                 = 0x00e7e9e3
	colorNotice                 = 0x00f4f7f3
	colorTabs                   = 0x00eceee9
	colorSelected               = 0x00f1f6ea
)

type repairMode int
type minMaxData struct{ Reserved, MaxSize, MaxPosition, MinTrack, MaxTrack point }
type nativeRect struct{ Left, Top, Right, Bottom int32 }
type paintData struct {
	DC              uintptr
	Erase           int32
	Rect            nativeRect
	Restore, Update int32
	Reserved        [32]byte
}
type drawItemData struct {
	Type, ID, ItemID, Action, State uint32
	Window, DC                      uintptr
	Rect                            nativeRect
	Data                            uintptr
}
type scrollData struct {
	Size, Mask uint32
	Min, Max   int32
	Page       uint32
	Pos, Track int32
}
type monitorData struct {
	Size          uint32
	Monitor, Work nativeRect
	Flags         uint32
}
type uiControl struct {
	hwnd       uintptr
	x, y, w, h int32
	role       int
	mode       repairMode
	key        string
}
type uiView struct {
	hwnd      uintptr
	dpi       int32
	fonts     [4]uintptr
	controls  []uiControl
	scroll    int32
	layingOut bool
}

var (
	postMessageProc     = u32.NewProc("PostMessageW")
	getClientRectProc   = u32.NewProc("GetClientRect")
	moveWindowProc      = u32.NewProc("MoveWindow")
	invalidateRectProc  = u32.NewProc("InvalidateRect")
	setScrollInfoProc   = u32.NewProc("SetScrollInfo")
	getScrollInfoProc   = u32.NewProc("GetScrollInfo")
	getDPIProc          = u32.NewProc("GetDpiForWindow")
	isDialogMessageProc = u32.NewProc("IsDialogMessageW")
	setFocusProc        = u32.NewProc("SetFocus")
	drawTextProc        = u32.NewProc("DrawTextW")
	selectObjectProc    = g32.NewProc("SelectObject")
	setTextColorProc    = g32.NewProc("SetTextColor")
	setBkColorProc      = g32.NewProc("SetBkColor")
	setBkModeProc       = g32.NewProc("SetBkMode")
	getStockObjectProc  = g32.NewProc("GetStockObject")
	setDCBrushColorProc = g32.NewProc("SetDCBrushColor")
)

// 所有 UTF-16 参数保留类型化引用，指针转换直接位于系统调用实参。
func text(h uintptr, s string) {
	p := pstr(s)
	setTextProc.Call(h, uintptr(unsafe.Pointer(p)))
	runtime.KeepAlive(p)
}
func alertFor(owner uintptr, title, body string, style uintptr) bool {
	b, t := pstr(body), pstr(title)
	r, _, _ := messageBox.Call(owner, uintptr(unsafe.Pointer(b)), uintptr(unsafe.Pointer(t)), style)
	runtime.KeepAlive(b)
	runtime.KeepAlive(t)
	return r == yes
}
func alert(title, body string, style uintptr) { alertFor(win, title, body, style) }
func ctrl(owner uintptr, class, title string, x, y, w, h int32, sty uint32, id uintptr) uintptr {
	c, t := pstr(class), pstr(title)
	hwnd, _, _ := createWindow.Call(0, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)), uintptr(wChild|wVisible|sty), uintptr(x), uintptr(y), uintptr(w), uintptr(h), owner, id, 0, 0)
	runtime.KeepAlive(c)
	runtime.KeepAlive(t)
	return hwnd
}
func scale(value, dpi int32) int32 { return (value*dpi + 48) / 96 }
func (v *uiView) init(h uintptr) {
	v.hwnd = h
	v.dpi = 96
	if getDPIProc.Find() == nil {
		d, _, _ := getDPIProc.Call(h)
		if d != 0 {
			v.dpi = int32(d)
		}
	}
	v.makeFonts()
}
func (v *uiView) makeFonts() {
	old := v.fonts
	face := pstr("Microsoft YaHei UI")
	for i, size := range []int32{15, 13, 22, 16} {
		weight := uintptr(400)
		if i >= 2 {
			weight = 600
		}
		height := -scale(size, v.dpi)
		v.fonts[i], _, _ = createFont.Call(uintptr(int64(height)), 0, 0, 0, weight, 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(face)))
	}
	runtime.KeepAlive(face)
	for _, c := range v.controls {
		sendMessage.Call(c.hwnd, wmFont, v.fonts[c.role], 1)
	}
	for _, f := range old {
		if f != 0 {
			deleteObject.Call(f)
		}
	}
}
func (v *uiView) close() {
	for _, f := range v.fonts {
		if f != 0 {
			deleteObject.Call(f)
		}
	}
	*v = uiView{}
}
func (v *uiView) add(class, title string, x, y, w, h int32, style uint32, id uintptr, role int, mode repairMode, key string) uintptr {
	hwnd := ctrl(v.hwnd, class, title, 0, 0, 1, 1, style, id)
	if hwnd == 0 {
		panic("无法创建界面控件")
	}
	v.controls = append(v.controls, uiControl{hwnd, x, y, w, h, role, mode, key})
	return hwnd
}
func (v *uiView) size() (int32, int32) {
	var r nativeRect
	getClientRectProc.Call(v.hwnd, uintptr(unsafe.Pointer(&r)))
	return r.Right * 96 / v.dpi, r.Bottom * 96 / v.dpi
}
func (v *uiView) place(c uiControl, x, y, w, h int32) {
	combo := c.key == "dllChoice" || c.key == "proxyChoice"
	if combo {
		h = 220
	}
	if c.key == "list" {
		sendMessage.Call(c.hwnd, 0x01a0, 0, uintptr(scale(48, v.dpi)))
	}
	moveWindowProc.Call(c.hwnd, uintptr(int64(scale(x, v.dpi))), uintptr(int64(scale(y, v.dpi))), uintptr(scale(w, v.dpi)), uintptr(scale(h, v.dpi)), 1)
	sendMessage.Call(c.hwnd, wmFont, v.fonts[c.role], 0)
	if combo {
		sendMessage.Call(c.hwnd, 0x0153, ^uintptr(0), uintptr(scale(28, v.dpi)))
		sendMessage.Call(c.hwnd, 0x0153, 0, uintptr(scale(32, v.dpi)))
		sendMessage.Call(c.hwnd, 0x0160, uintptr(scale(680, v.dpi)), 0)
	}
}
func (v *uiView) layoutMain() {
	if v.hwnd == 0 || v.layingOut {
		return
	}
	v.layingOut = true
	defer func() { v.layingOut = false }()
	w, h := v.size()
	maxScroll := contentHeight - h
	if maxScroll < 0 {
		maxScroll = 0
	}
	if v.scroll < 0 {
		v.scroll = 0
	}
	if v.scroll > maxScroll {
		v.scroll = maxScroll
	}
	si := scrollData{Size: uint32(unsafe.Sizeof(scrollData{})), Mask: 0x17, Max: contentHeight - 1, Page: uint32(h), Pos: v.scroll}
	setScrollInfoProc.Call(v.hwnd, 1, uintptr(unsafe.Pointer(&si)), 1)
	w, _ = v.size()
	extraHeight := h - contentHeight
	if extraHeight < 0 {
		extraHeight = 0
	}
	for _, c := range v.controls {
		x, y, cw, ch := c.x, c.y, c.w, c.h
		if x < 0 {
			x = w + x
		}
		if cw < 0 {
			cw = w + cw - x
		}
		if c.key == "choiceDLL" {
			cw = (w - 44) / 2
		}
		if c.key == "choiceYAML" {
			x = 24 + (w-44)/2
			cw = (w - 44) / 2
		}
		if len(c.key) == 11 && c.key[:10] == "yamlAction" {
			cw = (w - 88) / 4
			x = 32 + int32(c.key[10]-'0')*(cw+8)
		}
		if c.key == "result" {
			ch += extraHeight
		}
		if c.key == "footer" {
			y += extraHeight
		}
		fixed := isChromeControl(c)
		if !fixed {
			y -= v.scroll
		}
		v.place(c, x, y, cw, ch)
		if !fixed {
			clipTop := captionHeight - y
			if clipTop < 0 {
				clipTop = 0
			}
			if clipTop > 0 {
				region, _, _ := g32.NewProc("CreateRectRgn").Call(0, uintptr(scale(clipTop, v.dpi)), uintptr(scale(cw, v.dpi)), uintptr(scale(ch, v.dpi)))
				if ok, _, _ := u32.NewProc("SetWindowRgn").Call(c.hwnd, region, 1); ok == 0 {
					deleteObject.Call(region)
				}
			} else {
				u32.NewProc("SetWindowRgn").Call(c.hwnd, 0, 1)
			}
		}
		visible := c.mode == modeAny || c.mode == currentMode
		visible = visible && (fixed || y+ch > captionHeight)
		show := uintptr(0)
		if visible {
			show = 5
		}
		showWindow.Call(c.hwnd, show)
	}
	invalidateRectProc.Call(v.hwnd, 0, 1)
}
func (v *uiView) layoutChooser() {
	if v.hwnd == 0 {
		return
	}
	w, h := v.size()
	detailHeight := int32(108)
	if h < 400 {
		detailHeight = 64
	}
	listHeight := h - 242 - detailHeight
	if listHeight < 30 {
		listHeight = 30
	}
	for _, c := range v.controls {
		x, y, cw, ch := c.x, c.y, c.w, c.h
		if x < 0 {
			x = w + x
		}
		if cw < 0 {
			cw = w + cw - x
		}
		switch c.key {
		case "list":
			ch = listHeight
		case "details":
			y = 92 + listHeight + 12
			ch = detailHeight
		case "hint":
			y = h - 122
		case "status":
			y = h - 91
		case "refresh":
			y = h - 53
		case "confirm":
			x = w - 340
			y = h - 53
		case "cancel":
			x = w - 132
			y = h - 53
		}
		v.place(c, x, y, cw, ch)
	}
	invalidateRectProc.Call(v.hwnd, 0, 1)
}
func controlColors(dc, control uintptr) uintptr {
	color := uintptr(colorWhite)
	ink := uintptr(colorInk)
	for _, view := range []*uiView{&mainView, &chooserView, &backupView} {
		for _, c := range view.controls {
			if c.hwnd == control {
				if view == &mainView && (c.y < mainPanelTop || c.y >= mainPanelBottom) && c.key != "result" && c.key != "resultTitle" {
					color = colorBackground
				}
				if c.key == "notice" {
					color = colorNotice
				}
				if c.role == 1 {
					ink = colorMuted
				}
			}
		}
	}
	setBkModeProc.Call(dc, 2)
	setBkColorProc.Call(dc, color)
	setTextColorProc.Call(dc, ink)
	setDCBrushColorProc.Call(dc, color)
	brush, _, _ := getStockObjectProc.Call(18)
	return brush
}
func minimumWindowSize(h uintptr, l unsafe.Pointer, isChooser bool) {
	style, _, _ := u32.NewProc("GetWindowLongPtrW").Call(h, ^uintptr(15))
	height := int32(240)
	if isChooser {
		height = 340
	}
	r := windowBounds(h, 600, height, uint32(style))
	info := (*minMaxData)(l)
	info.MinTrack = point{r.Right - r.Left, r.Bottom - r.Top}
	monitor, _, _ := u32.NewProc("MonitorFromWindow").Call(h, 2)
	work := monitorData{Size: uint32(unsafe.Sizeof(monitorData{}))}
	if ok, _, _ := u32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&work))); ok != 0 {
		info.MaxPosition = point{work.Work.Left - work.Monitor.Left, work.Work.Top - work.Monitor.Top}
		info.MaxSize = point{work.Work.Right - work.Work.Left, work.Work.Bottom - work.Work.Top}
	}
}
func rounded(dc uintptr, r nativeRect, fill, border uintptr, dpi int32) {
	brush, _, _ := g32.NewProc("CreateSolidBrush").Call(fill)
	pen, _, _ := g32.NewProc("CreatePen").Call(0, uintptr(scale(1, dpi)), border)
	oldBrush, _, _ := selectObjectProc.Call(dc, brush)
	oldPen, _, _ := selectObjectProc.Call(dc, pen)
	g32.NewProc("RoundRect").Call(dc, uintptr(int64(r.Left)), uintptr(int64(r.Top)), uintptr(int64(r.Right)), uintptr(int64(r.Bottom)), uintptr(scale(12, dpi)), uintptr(scale(12, dpi)))
	selectObjectProc.Call(dc, oldBrush)
	selectObjectProc.Call(dc, oldPen)
	deleteObject.Call(brush)
	deleteObject.Call(pen)
}
func drawLabel(dc uintptr, s string, r nativeRect, color, font uintptr, flags uintptr) {
	p := pstr(s)
	old, _, _ := selectObjectProc.Call(dc, font)
	setBkModeProc.Call(dc, 1)
	setTextColorProc.Call(dc, color)
	drawTextProc.Call(dc, uintptr(unsafe.Pointer(p)), ^uintptr(0), uintptr(unsafe.Pointer(&r)), flags)
	runtime.KeepAlive(p)
	selectObjectProc.Call(dc, old)
}
func drawButton(l unsafe.Pointer) uintptr {
	d := (*drawItemData)(l)
	if d.ID >= chromeMin && d.ID <= chromeClose {
		return drawChrome(l)
	}
	view := &mainView
	if d.Window == nicConfirmButton || d.Window == nicRefreshButton || d.Window == nicCancelButton {
		view = &chooserView
	}
	if parent, _, _ := u32.NewProc("GetParent").Call(d.Window); parent == backupWindow && backupWindow != 0 {
		view = &backupView
	}
	fill, border, ink := uintptr(colorWhite), uintptr(colorBorder), uintptr(colorInk)
	isChoice := d.ID == chooseDLLMode || d.ID == chooseYAMLMode
	chosen := (d.ID == chooseDLLMode && currentMode == modeDLL) || (d.ID == chooseYAMLMode && currentMode == modeYAML)
	primary := d.ID == renameDll || d.ID == openChooser || d.ID == chooseConfirm || d.ID == backupConfirmID
	if isChoice {
		fill, border, ink = colorTabs, colorTabs, colorMuted
	}
	if chosen {
		fill = colorBlue
		border = colorBlue
		ink = colorWhite
	}
	if primary {
		fill = colorBlue
		border = colorBlue
		ink = colorWhite
	}
	if d.State&4 != 0 {
		fill = colorBorder
		border = colorBorder
		ink = colorMuted
	}
	if d.State&1 != 0 {
		fill = 0x0049600a
		ink = colorWhite
	}
	background := uintptr(colorWhite)
	if isChoice {
		background = colorTabs
	} else if d.ID == helpID {
		background = colorBackground
	}
	setDCBrushColorProc.Call(d.DC, background)
	brush, _, _ := getStockObjectProc.Call(18)
	u32.NewProc("FillRect").Call(d.DC, uintptr(unsafe.Pointer(&d.Rect)), brush)
	rounded(d.DC, d.Rect, fill, border, view.dpi)
	buf := make([]uint16, 512)
	u32.NewProc("GetWindowTextW").Call(d.Window, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	title := syscall.UTF16ToString(buf)
	r := d.Rect
	if isChoice {
		drawLabel(d.DC, title, r, ink, view.fonts[3], 0x25)
	} else {
		drawLabel(d.DC, title, r, ink, view.fonts[0], 0x25)
	}
	if d.State&0x10 != 0 {
		r = d.Rect
		r.Left += 4
		r.Top += 4
		r.Right -= 4
		r.Bottom -= 4
		u32.NewProc("DrawFocusRect").Call(d.DC, uintptr(unsafe.Pointer(&r)))
	}
	return 1
}
func paintMain(h uintptr) {
	var p paintData
	dc, _, _ := u32.NewProc("BeginPaint").Call(h, uintptr(unsafe.Pointer(&p)))
	w, clientHeight := mainView.size()
	dpi := mainView.dpi
	box := func(x, y, right, bottom int32) nativeRect {
		return nativeRect{scale(x, dpi), scale(y-mainView.scroll, dpi), scale(right, dpi), scale(bottom-mainView.scroll, dpi)}
	}
	saved, _, _ := g32.NewProc("SaveDC").Call(dc)
	g32.NewProc("IntersectClipRect").Call(dc, 0, uintptr(scale(captionHeight, dpi)), uintptr(scale(w, dpi)), uintptr(scale(clientHeight, dpi)))
	rounded(dc, box(16, 52, w-16, 90), colorTabs, colorTabs, dpi)
	rounded(dc, box(16, mainPanelTop, w-16, mainPanelBottom), colorWhite, colorBorder, dpi)
	if currentMode != modeNone {
		rounded(dc, box(32, 274, w-32, 328), colorNotice, colorNotice, dpi)
	}
	extra := clientHeight - contentHeight
	if extra < 0 {
		extra = 0
	}
	rounded(dc, box(16, 396, w-16, 480+extra), colorWhite, colorBorder, dpi)
	g32.NewProc("RestoreDC").Call(dc, saved)
	fillRect(dc, nativeRect{0, 0, scale(w, dpi), scale(captionHeight, dpi)}, colorBackground)
	logo := nativeRect{scale(16, dpi), scale(9, dpi), scale(40, dpi), scale(33, dpi)}
	rounded(dc, logo, colorBlue, colorBlue, dpi)
	drawLabel(dc, "↔", logo, colorWhite, mainView.fonts[0], 0x25)
	u32.NewProc("EndPaint").Call(h, uintptr(unsafe.Pointer(&p)))
}
func enable(h uintptr, on bool) {
	value := uintptr(0)
	if on {
		value = 1
	}
	enableWindow.Call(h, value)
}
func handleDPI(view *uiView, w uintptr, l unsafe.Pointer) {
	view.dpi = int32(w & 0xffff)
	view.makeFonts()
	r := (*nativeRect)(l)
	u32.NewProc("SetWindowPos").Call(view.hwnd, 0, uintptr(int64(r.Left)), uintptr(int64(r.Top)), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), 0x14)
}
func windowBounds(owner uintptr, clientWidth, clientHeight int32, _ uint32) nativeRect {
	monitor, _, _ := u32.NewProc("MonitorFromWindow").Call(owner, 2)
	info := monitorData{Size: uint32(unsafe.Sizeof(monitorData{}))}
	u32.NewProc("GetMonitorInfoW").Call(monitor, uintptr(unsafe.Pointer(&info)))
	if info.Work.Right <= info.Work.Left {
		info.Work = nativeRect{0, 0, 1024, 768}
	}
	dpi := int32(96)
	proc := syscall.NewLazyDLL("shcore.dll").NewProc("GetDpiForMonitor")
	if proc.Find() == nil {
		var x, y uint32
		if r, _, _ := proc.Call(monitor, 0, uintptr(unsafe.Pointer(&x)), uintptr(unsafe.Pointer(&y))); r == 0 && x != 0 {
			dpi = int32(x)
		}
	}
	r := nativeRect{0, 0, scale(clientWidth, dpi), scale(clientHeight, dpi)}
	w, h := r.Right-r.Left, r.Bottom-r.Top
	if max := info.Work.Right - info.Work.Left - 24; w > max {
		w = max
	}
	if max := info.Work.Bottom - info.Work.Top - 24; h > max {
		h = max
	}
	x, y := info.Work.Left+(info.Work.Right-info.Work.Left-w)/2, info.Work.Top+(info.Work.Bottom-info.Work.Top-h)/2
	return nativeRect{x, y, x + w, y + h}
}
func registerNativeClass(name string, callback uintptr, bg uintptr) {
	class := pstr(name)
	inst, _, _ := getModule.Call(0)
	cursor, _, _ := loadCursor.Call(0, 32512)
	d := classDef{Size: uint32(unsafe.Sizeof(classDef{})), Style: 8, Proc: callback, Instance: inst, Cursor: cursor, Background: bg, ClassName: class}
	r, _, err := regClass.Call(uintptr(unsafe.Pointer(&d)))
	runtime.KeepAlive(class)
	runtime.KeepAlive(d)
	if r == 0 && err != syscall.Errno(1410) {
		panic(err)
	}
}
func createNativeWindow(name, title string, owner uintptr, width, height int32, style uint32) uintptr {
	r := windowBounds(owner, width, height, style)
	c, t := pstr(name), pstr(title)
	inst, _, _ := getModule.Call(0)
	h, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(c)), uintptr(unsafe.Pointer(t)), uintptr(style), uintptr(int64(r.Left)), uintptr(int64(r.Top)), uintptr(r.Right-r.Left), uintptr(r.Bottom-r.Top), owner, 0, inst, 0)
	runtime.KeepAlive(c)
	runtime.KeepAlive(t)
	if h == 0 {
		panic(err)
	}
	return h
}

//go:build windows

package main

import (
	"strings"
	"syscall"
	"unsafe"
)

const (
	chromeMin     = 601
	chromeMax     = 602
	chromeClose   = 603
	captionHeight = 42
)

var dwm = syscall.NewLazyDLL("dwmapi.dll")

func isChromeControl(c uiControl) bool {
	return c.key == "caption" || strings.HasPrefix(c.key, "chrome")
}

func addChrome(v *uiView, title string, canMinimize bool) {
	v.add("STATIC", title, 50, 9, -228, 24, 0, 0, 3, modeAny, "caption")
	if canMinimize {
		v.add("BUTTON", "最小化", -120, 0, 40, 40, bsOwnerDraw|wTab, chromeMin, 0, modeAny, "chromeMin")
	}
	v.add("BUTTON", "最大化 / 还原", -80, 0, 40, 40, bsOwnerDraw|wTab, chromeMax, 0, modeAny, "chromeMax")
	v.add("BUTTON", "关闭", -40, 0, 40, 40, bsOwnerDraw|wTab, chromeClose, 0, modeAny, "chromeClose")
}

func chromeCommand(h uintptr, id uintptr) bool {
	switch id {
	case chromeMin:
		showWindow.Call(h, 6)
	case chromeMax:
		zoomed, _, _ := u32.NewProc("IsZoomed").Call(h)
		mode := uintptr(3)
		if zoomed != 0 {
			mode = 9
		}
		showWindow.Call(h, mode)
	case chromeClose:
		postMessageProc.Call(h, 0x0010, 0, 0)
	default:
		return false
	}
	return true
}

func setupFrame(h uintptr) {
	updateFrame(h)
	corner := uint32(2)
	dwm.NewProc("DwmSetWindowAttribute").Call(h, 33, uintptr(unsafe.Pointer(&corner)), 4)
	u32.NewProc("SetWindowPos").Call(h, 0, 0, 0, 0, 0, 0x0037)
}

func updateFrame(h uintptr) {
	margins := [4]int32{1, 1, 1, 1}
	dwm.NewProc("DwmExtendFrameIntoClientArea").Call(h, uintptr(unsafe.Pointer(&margins)))
}

func paintChooserChrome(h uintptr) {
	paintViewChrome(&chooserView, h)
}

func paintViewChrome(view *uiView, h uintptr) {
	var p paintData
	dc, _, _ := u32.NewProc("BeginPaint").Call(h, uintptr(unsafe.Pointer(&p)))
	dpi := view.dpi
	logo := nativeRect{scale(16, dpi), scale(9, dpi), scale(40, dpi), scale(33, dpi)}
	rounded(dc, logo, colorBlue, colorBlue, dpi)
	drawLabel(dc, "↔", logo, colorWhite, view.fonts[0], 0x25)
	u32.NewProc("EndPaint").Call(h, uintptr(unsafe.Pointer(&p)))
}

func frameMessage(view *uiView, h uintptr, m uint32, w uintptr, l unsafe.Pointer) (uintptr, bool) {
	switch m {
	case 0x0083: // WM_NCCALCSIZE: 用客户区绘制整个窗口，保留原生窗口样式和系统命令。
		return 0, true
	case 0x0006, 0x031e: // WM_ACTIVATE / WM_DWMCOMPOSITIONCHANGED
		updateFrame(h)
	case 0x0084: // WM_NCHITTEST
		var r nativeRect
		u32.NewProc("GetWindowRect").Call(h, uintptr(unsafe.Pointer(&r)))
		x, y := int32(int16(uintptr(l)&0xffff))-r.Left, int32(int16(uintptr(l)>>16))-r.Top
		dpi := view.dpi
		if dpi <= 0 {
			dpi = 96
		}
		width, height := r.Right-r.Left, r.Bottom-r.Top
		edge := scale(6, dpi)
		zoomed, _, _ := u32.NewProc("IsZoomed").Call(h)
		if zoomed == 0 {
			switch {
			case y < edge && x < edge:
				return 13, true
			case y < edge && x >= width-edge:
				return 14, true
			case y >= height-edge && x < edge:
				return 16, true
			case y >= height-edge && x >= width-edge:
				return 17, true
			case x < edge:
				return 10, true
			case x >= width-edge:
				return 11, true
			case y < edge:
				return 12, true
			case y >= height-edge:
				return 15, true
			}
		}
		if y < scale(captionHeight, dpi) {
			for _, c := range view.controls {
				if !strings.HasPrefix(c.key, "chrome") {
					continue
				}
				var bounds nativeRect
				u32.NewProc("GetWindowRect").Call(c.hwnd, uintptr(unsafe.Pointer(&bounds)))
				px, py := x+r.Left, y+r.Top
				if px >= bounds.Left && px < bounds.Right && py >= bounds.Top && py < bounds.Bottom {
					return 1, true
				}
			}
			return 2, true // HTCAPTION: 系统处理拖动、双击最大化和系统菜单。
		}
		return 1, true
	}
	return 0, false
}

func drawChrome(l unsafe.Pointer) uintptr {
	d := (*drawItemData)(l)
	view := &mainView
	parent, _, _ := u32.NewProc("GetParent").Call(d.Window)
	if parent == chooser {
		view = &chooserView
	}
	if parent == backupWindow && backupWindow != 0 {
		view = &backupView
	}
	fill, ink := uintptr(colorBackground), uintptr(colorInk)
	if view != &mainView {
		fill = colorWhite
	}
	if d.State&1 != 0 {
		fill = colorTabs
	}
	if d.ID == chromeClose && d.State&1 != 0 {
		fill, ink = 0x004444d4, colorWhite
	}
	fillRect(d.DC, d.Rect, fill)
	dpi := view.dpi
	x, y := (d.Rect.Right+d.Rect.Left)/2, (d.Rect.Bottom+d.Rect.Top)/2
	s := scale(5, dpi)
	switch d.ID {
	case chromeMin:
		stroke(d.DC, ink, scale(1, dpi), point{x - s, y}, point{x + s, y})
	case chromeMax:
		stroke(d.DC, ink, scale(1, dpi), point{x - s, y - s}, point{x + s, y - s}, point{x + s, y + s}, point{x - s, y + s}, point{x - s, y - s})
	case chromeClose:
		stroke(d.DC, ink, scale(1, dpi), point{x - s, y - s}, point{x + s, y + s})
		stroke(d.DC, ink, scale(1, dpi), point{x + s, y - s}, point{x - s, y + s})
	}
	if d.State&0x10 != 0 {
		r := d.Rect
		r.Left += scale(5, dpi)
		r.Right -= scale(5, dpi)
		r.Top += scale(5, dpi)
		r.Bottom -= scale(5, dpi)
		u32.NewProc("DrawFocusRect").Call(d.DC, uintptr(unsafe.Pointer(&r)))
	}
	return 1
}

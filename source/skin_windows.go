//go:build windows

package main

import (
	"runtime"
	"syscall"
	"unsafe"
)

const skinCombo = 1
const skinCheckbox = 2

var (
	subclassCallback uintptr
	comctl           = syscall.NewLazyDLL("comctl32.dll")
	setSubclass      = comctl.NewProc("SetWindowSubclass")
	defSubclass      = comctl.NewProc("DefSubclassProc")
	removeSubclass   = comctl.NewProc("RemoveWindowSubclass")
)

// 只接管绘制；选择、勾选、键盘和原生控件消息仍交给 Windows。
func skinControl(h uintptr, kind uintptr) {
	if subclassCallback == 0 {
		subclassCallback = syscall.NewCallback(skinProc)
	}
	if ok, _, err := setSubclass.Call(h, subclassCallback, kind, 0); ok == 0 {
		panic(err)
	}
}

func skinProc(h uintptr, m uint32, w uintptr, l unsafe.Pointer, kind, ref uintptr) uintptr {
	switch m {
	case wmPaint:
		var p paintData
		dc, _, _ := u32.NewProc("BeginPaint").Call(h, uintptr(unsafe.Pointer(&p)))
		paintSkin(h, dc, kind)
		u32.NewProc("EndPaint").Call(h, uintptr(unsafe.Pointer(&p)))
		return 0
	case 0x0318: // WM_PRINTCLIENT
		paintSkin(h, w, kind)
		return 0
	case 0x0014: // WM_ERASEBKGND
		return 1
	case 0x0082: // WM_NCDESTROY
		removeSubclass.Call(h, subclassCallback, kind)
	}
	r, _, _ := defSubclass.Call(h, uintptr(m), w, uintptr(l))
	switch m {
	case 0x0007, 0x0008, 0x000a, 0x0101, 0x0202, 0x00f1, 0x014e, 0x014b, 0x0143, 0x014f, 0x0153:
		invalidateRectProc.Call(h, 0, 1)
	}
	return r
}

func comboLabel(h uintptr, index uintptr) string {
	if int32(index) < 0 {
		return "请选择"
	}
	n, _, _ := sendMessage.Call(h, 0x0149, index, 0) // CB_GETLBTEXTLEN
	if int32(n) < 0 || n > 32767 {
		return "请查看执行结果中的完整路径"
	}
	buf := make([]uint16, n+1)
	sendMessage.Call(h, 0x0148, index, uintptr(unsafe.Pointer(&buf[0])))
	return syscall.UTF16ToString(buf)
}

func fillRect(dc uintptr, r nativeRect, color uintptr) {
	setDCBrushColorProc.Call(dc, color)
	brush, _, _ := getStockObjectProc.Call(18)
	u32.NewProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&r)), brush)
}

func stroke(dc uintptr, color uintptr, width int32, points ...point) {
	if len(points) < 2 {
		return
	}
	pen, _, _ := g32.NewProc("CreatePen").Call(0, uintptr(width), color)
	old, _, _ := selectObjectProc.Call(dc, pen)
	g32.NewProc("Polyline").Call(dc, uintptr(unsafe.Pointer(&points[0])), uintptr(len(points)))
	runtime.KeepAlive(points)
	selectObjectProc.Call(dc, old)
	deleteObject.Call(pen)
}

func paintSkin(h, dc, kind uintptr) {
	var r nativeRect
	getClientRectProc.Call(h, uintptr(unsafe.Pointer(&r)))
	fillRect(dc, r, colorWhite)
	dpi := mainView.dpi
	if dpi <= 0 {
		dpi = 96
	}
	focus, _, _ := u32.NewProc("GetFocus").Call()
	enabled, _, _ := u32.NewProc("IsWindowEnabled").Call(h)
	ink, border := uintptr(colorInk), uintptr(colorBorder)
	if focus == h {
		border = colorBlue
	}
	if enabled == 0 {
		ink = colorMuted
	}
	if kind == skinCombo {
		r.Right--
		r.Bottom--
		rounded(dc, r, colorWhite, border, dpi)
		index, _, _ := sendMessage.Call(h, comboGetSelection, 0, 0)
		label := r
		label.Left += scale(10, dpi)
		label.Right -= scale(32, dpi)
		drawLabel(dc, comboLabel(h, index), label, ink, mainView.fonts[0], 0x8024)
		x, y := r.Right-scale(17, dpi), r.Bottom/2
		stroke(dc, ink, scale(1, dpi), point{x - scale(4, dpi), y - scale(2, dpi)}, point{x, y + scale(2, dpi)}, point{x + scale(4, dpi), y - scale(2, dpi)})
		return
	}
	if focus == h {
		outer := r
		outer.Right--
		outer.Bottom--
		rounded(dc, outer, colorSelected, colorSelected, dpi)
	}
	box := nativeRect{scale(2, dpi), (r.Bottom - scale(18, dpi)) / 2, scale(20, dpi), (r.Bottom + scale(18, dpi)) / 2}
	check, _, _ := sendMessage.Call(h, checkGet, 0, 0)
	fill := uintptr(colorWhite)
	if check == checked {
		fill, border = colorBlue, colorBlue
	}
	rounded(dc, box, fill, border, dpi)
	if check == checked {
		x, y := box.Left, box.Top
		stroke(dc, colorWhite, scale(2, dpi), point{x + scale(4, dpi), y + scale(9, dpi)}, point{x + scale(8, dpi), y + scale(13, dpi)}, point{x + scale(14, dpi), y + scale(5, dpi)})
	}
	buf := make([]uint16, 512)
	u32.NewProc("GetWindowTextW").Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	label := r
	label.Left = scale(30, dpi)
	drawLabel(dc, syscall.UTF16ToString(buf), label, ink, mainView.fonts[0], 0x8024)
}

func drawComboItem(l unsafe.Pointer) uintptr {
	d := (*drawItemData)(l)
	fill, ink := uintptr(colorWhite), uintptr(colorInk)
	if d.State&1 != 0 {
		fill, ink = colorSelected, colorBlue
	}
	fillRect(d.DC, d.Rect, fill)
	r := d.Rect
	r.Left += scale(10, mainView.dpi)
	r.Right -= scale(10, mainView.dpi)
	drawLabel(d.DC, comboLabel(d.Window, uintptr(d.ItemID)), r, ink, mainView.fonts[0], 0x8024)
	return 1
}

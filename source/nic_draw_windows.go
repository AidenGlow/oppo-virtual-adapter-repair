//go:build windows

package main

import (
	"strings"
	"unsafe"
)

type measureItemData struct {
	Type, ID, ItemID, Width, Height uint32
	Data                            uintptr
}

func nicRowDetails(a Adapter) string {
	kind := "类型未知"
	if a.HardwareKnown {
		kind = "虚拟网卡"
		if a.Hardware {
			kind = "物理网卡"
		}
	}
	if a.HasRoute {
		kind += " · 默认路由"
	}
	if a.Description != "" {
		kind += " · " + a.Description
	}
	return kind
}

func drawNICItem(l unsafe.Pointer) uintptr {
	d := (*drawItemData)(l)
	fill := uintptr(colorWhite)
	if d.State&1 != 0 {
		fill = colorSelected
	}
	setDCBrushColorProc.Call(d.DC, fill)
	brush, _, _ := getStockObjectProc.Call(18)
	u32.NewProc("FillRect").Call(d.DC, uintptr(unsafe.Pointer(&d.Rect)), brush)
	if int32(d.ItemID) < 0 || int(d.ItemID) >= len(adapters) {
		return 1
	}
	a := adapters[d.ItemID]
	dpi := chooserView.dpi
	name := d.Rect
	name.Left += scale(12, dpi)
	name.Right -= scale(140, dpi)
	name.Top += scale(4, dpi)
	name.Bottom = name.Top + scale(22, dpi)
	drawLabel(d.DC, a.Name, name, colorInk, chooserView.fonts[3], 0x8024)
	state := name
	state.Left = name.Right + scale(8, dpi)
	state.Right = d.Rect.Right - scale(12, dpi)
	ink := uintptr(colorMuted)
	if strings.EqualFold(a.Status, "Up") {
		ink = colorBlue
	}
	drawLabel(d.DC, adapterState(a), state, ink, chooserView.fonts[1], 0x8026)
	detail := d.Rect
	detail.Left += scale(12, dpi)
	detail.Right -= scale(12, dpi)
	detail.Top += scale(27, dpi)
	detail.Bottom -= scale(3, dpi)
	drawLabel(d.DC, nicRowDetails(a), detail, colorMuted, chooserView.fonts[1], 0x8024)
	if d.State&0x10 != 0 {
		focus := d.Rect
		focus.Left += 2
		focus.Top += 2
		focus.Right -= 2
		focus.Bottom -= 2
		u32.NewProc("DrawFocusRect").Call(d.DC, uintptr(unsafe.Pointer(&focus)))
	}
	return 1
}

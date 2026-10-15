//go:build windows

package main

import (
	"bytes"
	_ "embed"
	"image/png"
	"runtime"
	"unsafe"
)

const (
	softwareRevIcon = 701
	softwareCFWIcon = 702
)

// 原始软件图标随 EXE 内嵌，运行时无需联网或安装对应客户端。
//
//go:embed assets/clash-verge-rev.png
var vergeIconPNG []byte

//go:embed assets/clash-for-windows.png
var cfwIconPNG []byte

type softwareIcon struct {
	width, height int32
	pixels        []byte
}

var softwareIcons = map[uint32]softwareIcon{
	softwareRevIcon: decodeSoftwareIcon(vergeIconPNG),
	softwareCFWIcon: decodeSoftwareIcon(cfwIconPNG),
}

func decodeSoftwareIcon(data []byte) softwareIcon {
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		panic("内嵌软件图标损坏：" + err.Error())
	}
	bounds := src.Bounds()
	icon := softwareIcon{int32(bounds.Dx()), int32(bounds.Dy()), make([]byte, bounds.Dx()*bounds.Dy()*4)}
	for y := 0; y < bounds.Dy(); y++ {
		for x := 0; x < bounds.Dx(); x++ {
			r, g, b, a := src.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
			// RGBA 返回预乘通道；透明部分合成到卡片的白色背景。
			i := (y*bounds.Dx() + x) * 4
			icon.pixels[i], icon.pixels[i+1], icon.pixels[i+2] = byte((b+0xffff-a)>>8), byte((g+0xffff-a)>>8), byte((r+0xffff-a)>>8)
		}
	}
	return icon
}

func drawSoftwareIcon(l unsafe.Pointer) uintptr {
	d := (*drawItemData)(l)
	icon, ok := softwareIcons[d.ID]
	if !ok {
		return 0
	}
	info := struct {
		Size                   uint32
		Width, Height          int32
		Planes, Bits           uint16
		Compression, ImageSize uint32
		XPels, YPels           int32
		Used, Important        uint32
	}{Size: 40, Width: icon.width, Height: -icon.height, Planes: 1, Bits: 32}
	old, _, _ := g32.NewProc("SetStretchBltMode").Call(d.DC, 4) // HALFTONE 高质量缩放。
	g32.NewProc("SetBrushOrgEx").Call(d.DC, 0, 0, 0)
	g32.NewProc("StretchDIBits").Call(d.DC, uintptr(d.Rect.Left), uintptr(d.Rect.Top), uintptr(d.Rect.Right-d.Rect.Left), uintptr(d.Rect.Bottom-d.Rect.Top), 0, 0, uintptr(icon.width), uintptr(icon.height), uintptr(unsafe.Pointer(&icon.pixels[0])), uintptr(unsafe.Pointer(&info)), 0, 0x00cc0020)
	g32.NewProc("SetStretchBltMode").Call(d.DC, old)
	runtime.KeepAlive(icon.pixels)
	return 1
}

//go:build windows

package main

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func nativeTextForTest(h uintptr) string {
	buf := make([]uint16, 2048)
	u32.NewProc("GetWindowTextW").Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func TestUIDPIAndResponsiveLayout(t *testing.T) {
	preventLiveDiscovery(t)
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		c := createChooserWindow()
		defer destroyWindow.Call(c)
		for _, dpi := range []int32{96, 120, 144, 192} {
			t.Logf("在同一 GUI 线程验证 DPI=%d", dpi)
			for _, view := range []*uiView{&mainView, &chooserView} {
				r := nativeRect{0, 0, scale(800, dpi), scale(750, dpi)}
				handleDPI(view, uintptr(dpi)|uintptr(dpi)<<16, unsafe.Pointer(&r))
				// 从实际 GDI 字体对象核对尺寸与 ClearType，而非检查源代码常量。
				var font struct {
					Height, Width, Escapement, Orientation, Weight int32
					Italic, Underline, StrikeOut, Charset          byte
					OutPrecision, ClipPrecision, Quality, Pitch    byte
					Face                                           [32]uint16
				}
				if n, _, _ := g32.NewProc("GetObjectW").Call(view.fonts[0], unsafe.Sizeof(font), uintptr(unsafe.Pointer(&font))); n == 0 {
					t.Fatal("未能读取原生字体")
				}
				if font.Height != -scale(15, dpi) || font.Quality != 5 {
					t.Fatalf("字体未按 DPI 重建：%+v", font)
				}
				for _, control := range view.controls {
					f, _, _ := sendMessage.Call(control.hwnd, 0x0031, 0, 0)
					if f != view.fonts[control.role] {
						t.Fatal("控件仍引用旧 DPI 字体")
					}
				}
			}
			for _, width := range []int32{600, 640, 1000} {
				moveWindowProc.Call(h, 0, 0, uintptr(scale(width, dpi)), uintptr(scale(750, dpi)), 0)
				for _, mode := range []repairMode{modeDLL, modeYAML} {
					setRepairMode(mode)
					assertStaticTextFits(t, &mainView)
				}
			}
			chooserView.layoutChooser()
			assertStaticTextFits(t, &chooserView)
		}
	})
}

func assertStaticTextFits(t *testing.T, view *uiView) {
	t.Helper()
	dc, _, _ := u32.NewProc("GetDC").Call(view.hwnd)
	defer u32.NewProc("ReleaseDC").Call(view.hwnd, dc)
	foundHint := view != &mainView
	for _, c := range view.controls {
		if !visibleStyleForTest(c.hwnd) {
			continue
		}
		class := make([]uint16, 32)
		u32.NewProc("GetClassNameW").Call(c.hwnd, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
		if syscall.UTF16ToString(class) != "Static" && syscall.UTF16ToString(class) != "STATIC" {
			continue
		}
		label := nativeTextForTest(c.hwnd)
		if strings.Contains(label, "如遇问题可点击恢复") {
			foundHint = true
		}
		var bounds nativeRect
		getClientRectProc.Call(c.hwnd, uintptr(unsafe.Pointer(&bounds)))
		measured := bounds
		textPtr := pstr(label)
		old, _, _ := selectObjectProc.Call(dc, view.fonts[c.role])
		drawTextProc.Call(dc, uintptr(unsafe.Pointer(textPtr)), ^uintptr(0), uintptr(unsafe.Pointer(&measured)), 0x400|0x10|0x800)
		selectObjectProc.Call(dc, old)
		if measured.Bottom > bounds.Bottom {
			t.Errorf("DPI=%d，文字被裁切：%q，需要 %d px，只有 %d px", view.dpi, label, measured.Bottom, bounds.Bottom)
		}
	}
	if !foundHint {
		t.Error("当前修复方式缺少恢复提示")
	}
}

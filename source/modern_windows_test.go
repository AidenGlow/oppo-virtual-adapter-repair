//go:build windows

package main

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

// 从实际 Win32 窗口绘制到内存位图，用于检查持久选中态，而非检查源码颜色常量。
func windowImageForTest(t *testing.T, h uintptr) *image.NRGBA {
	t.Helper()
	root, _, _ := u32.NewProc("GetAncestor").Call(h, 2)
	visible, _, _ := u32.NewProc("IsWindowVisible").Call(root)
	if visible == 0 {
		outside := int32(-20000)
		u32.NewProc("SetWindowPos").Call(root, 0, uintptr(int64(outside)), uintptr(int64(outside)), 0, 0, 0x15)
		showWindow.Call(root, 4)
		defer showWindow.Call(root, 0)
	}
	u32.NewProc("RedrawWindow").Call(root, 0, 0, 0x0185)
	var r nativeRect
	getClientRectProc.Call(h, uintptr(unsafe.Pointer(&r)))
	width, height := int(r.Right), int(r.Bottom)
	if width <= 0 || height <= 0 {
		t.Fatal("无效窗口尺寸")
	}
	var info struct {
		Size                   uint32
		Width, Height          int32
		Planes, Bits           uint16
		Compression, ImageSize uint32
		XPels, YPels           int32
		Used, Important        uint32
	}
	info.Size, info.Width, info.Height, info.Planes, info.Bits = 40, int32(width), -int32(height), 1, 32
	screen, _, _ := u32.NewProc("GetDC").Call(h)
	dc, _, _ := g32.NewProc("CreateCompatibleDC").Call(screen)
	var pixels unsafe.Pointer
	bitmap, _, err := g32.NewProc("CreateDIBSection").Call(screen, uintptr(unsafe.Pointer(&info)), 0, uintptr(unsafe.Pointer(&pixels)), 0, 0)
	if bitmap == 0 || pixels == nil {
		t.Fatal("无法建立截图位图", err)
	}
	old, _, _ := selectObjectProc.Call(dc, bitmap)
	defer func() {
		selectObjectProc.Call(dc, old)
		deleteObject.Call(bitmap)
		g32.NewProc("DeleteDC").Call(dc)
		u32.NewProc("ReleaseDC").Call(h, screen)
	}()
	if ok, _, err := u32.NewProc("PrintWindow").Call(h, dc, 1); ok == 0 {
		t.Fatal("无法绘制测试窗口", err)
	}
	g32.NewProc("GdiFlush").Call()
	raw := unsafe.Slice((*byte)(pixels), width*height*4)
	out := image.NewNRGBA(image.Rect(0, 0, width, height))
	for i := 0; i < len(raw); i += 4 {
		out.Pix[i], out.Pix[i+1], out.Pix[i+2], out.Pix[i+3] = raw[i+2], raw[i+1], raw[i], 255
	}
	return out
}

func saveWindowForTest(t *testing.T, h uintptr, name string) {
	t.Helper()
	dir := os.Getenv("OPPO_REPAIR_UI_CAPTURE_DIR")
	if dir == "" {
		return
	}
	file, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	err = png.Encode(file, windowImageForTest(t, h))
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		t.Fatal(err, closeErr)
	}
}

func TestRepairModeHighlightPersistsAfterFocusAndRepaint(t *testing.T) {
	preventLiveDiscovery(t)
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		for _, mode := range []repairMode{modeDLL, modeYAML, modeDLL} {
			setRepairMode(mode)
			stopPathSearch()
			for _, control := range mainView.controls {
				if control.key != "choiceDLL" && control.key != "choiceYAML" {
					continue
				}
				isSelected := (control.key == "choiceDLL" && mode == modeDLL) || (control.key == "choiceYAML" && mode == modeYAML)
				for i := 0; i < 3; i++ {
					sendMessage.Call(control.hwnd, 0x0008, statusText, 0)
					invalidateRectProc.Call(control.hwnd, 0, 1)
					mainView.layoutMain()
					shot := windowImageForTest(t, h)
					var bounds nativeRect
					u32.NewProc("GetWindowRect").Call(control.hwnd, uintptr(unsafe.Pointer(&bounds)))
					u32.NewProc("MapWindowPoints").Call(0, h, uintptr(unsafe.Pointer(&bounds)), 2)
					pixel := shot.NRGBAAt(int(bounds.Left+scale(10, mainView.dpi)), int((bounds.Top+bounds.Bottom)/2))
					green := pixel.R == 0x0b && pixel.G == 0x80 && pixel.B == 0x60
					if green != isSelected {
						t.Fatalf("方式 %d 失去焦点 / 重绘 %d 后高亮错误：%v", mode, i, pixel)
					}
				}
			}
		}
		saveWindowForTest(t, h, "ui-dll-persistent.png")
	})
}

func TestModernControlsPreserveNativeInteraction(t *testing.T) {
	preventLiveDiscovery(t)
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		setRepairMode(modeDLL)
		useManualDLL(`C:\OPPO\daemon\wintun.dll`)
		if enabledForTest(renameButton) {
			t.Fatal("未勾选即可修复")
		}
		sendMessage.Call(checkBox, 0x00f5, 0, 0) // BM_CLICK 仍由原生自动复选框处理。
		check, _, _ := sendMessage.Call(checkBox, checkGet, 0, 0)
		if check != checked || !enabledForTest(renameButton) {
			t.Fatal("现代复选框未同步原生勾选和安全门槛")
		}
		saveWindowForTest(t, h, "ui-dll-checked-sample.png")
		sendMessage.Call(checkBox, 0x0100, 0x20, 0)
		sendMessage.Call(checkBox, 0x0101, 0x20, 0)
		check, _, _ = sendMessage.Call(checkBox, checkGet, 0, 0)
		if check != 0 || enabledForTest(renameButton) {
			t.Fatal("空格键未取消勾选或仍允许修复")
		}
		setRepairMode(modeYAML)
		stopPathSearch()
		proxyCandidates = []pathCandidate{
			{Client: clientRev, Path: `C:\Rev\config.yaml`, Label: "Clash Verge Rev"},
			{Client: clientCFW, Path: `C:\CFW\profiles\current.yml`, Label: "Clash for Windows"},
		}
		fillPathChoices(yamlText, "选择客户端", proxyCandidates)
		for _, client := range []string{clientRev, clientCFW} {
			sendMessage.Call(yamlText, 0x0100, 0x28, 0)
			if selectedProxy == nil || selectedProxy.Client != client || !enabledForTest(openChooserButton) {
				t.Fatal("下拉框键盘选择或父窗口通知失效", client)
			}
		}
		if !strings.Contains(comboLabel(yamlText, 2), "Clash for Windows") {
			t.Fatal("下拉框丢失可读取的原生文字")
		}
		saveWindowForTest(t, h, "ui-yaml-selection-sample.png")
		setRepairMode(modeDLL)
		check, _, _ = sendMessage.Call(checkBox, checkGet, 0, 0)
		if check != 0 || enabledForTest(renameButton) {
			t.Fatal("返回 DLL 方式错误保留勾选")
		}
		c := createChooserWindow()
		defer destroyWindow.Call(c)
		adapters = []Adapter{{Name: "WLAN", Description: "Wireless Network Adapter", Status: "Up", Hardware: true, HardwareKnown: true, HasRoute: true}}
		row := pstr("WLAN | 物理网卡 | 已连接 · 默认路由")
		sendMessage.Call(nicList, listAdd, 0, uintptr(unsafe.Pointer(row)))
		saveWindowForTest(t, c, "ui-network-frame-sample.png")
	})
}

func TestCustomFrameHitTestingAndWindowCommands(t *testing.T) {
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		var bounds, client nativeRect
		u32.NewProc("GetWindowRect").Call(h, uintptr(unsafe.Pointer(&bounds)))
		getClientRectProc.Call(h, uintptr(unsafe.Pointer(&client)))
		if bounds.Right-bounds.Left != client.Right || bounds.Bottom-bounds.Top != client.Bottom {
			t.Fatal("仍有旧式非客户区标题栏", bounds, client)
		}
		hit := func(x, y int32) uintptr {
			packed := uintptr(uint16(x)) | uintptr(uint16(y))<<16
			r, _, _ := sendMessage.Call(h, 0x0084, 0, packed)
			return r
		}
		if hit(bounds.Left+scale(230, mainView.dpi), bounds.Top+scale(20, mainView.dpi)) != 2 {
			t.Fatal("标题空白区不能系统拖动")
		}
		if hit(bounds.Left+1, bounds.Top+scale(200, mainView.dpi)) != 10 {
			t.Fatal("窗口左边界不能调整大小")
		}
		if hit(bounds.Right-scale(20, mainView.dpi), bounds.Top+scale(20, mainView.dpi)) != 1 {
			t.Fatal("关闭按钮区域被误判为拖动区")
		}
		chromeCommand(h, chromeMax)
		zoomed, _, _ := u32.NewProc("IsZoomed").Call(h)
		showWindow.Call(h, 0)
		if zoomed == 0 {
			t.Fatal("最大化按钮没有生效")
		}
		chromeCommand(h, chromeMax)
		zoomed, _, _ = u32.NewProc("IsZoomed").Call(h)
		showWindow.Call(h, 0)
		if zoomed != 0 {
			t.Fatal("还原按钮没有生效")
		}
		chromeCommand(h, chromeMin)
		iconic, _, _ := u32.NewProc("IsIconic").Call(h)
		showWindow.Call(h, 0)
		if iconic == 0 {
			t.Fatal("最小化按钮没有生效")
		}
	})
}

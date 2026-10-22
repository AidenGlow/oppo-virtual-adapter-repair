//go:build windows

package main

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestNICRedesignKeepsNativeSelectionAndText(t *testing.T) {
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		c := createChooserWindow()
		defer destroyWindow.Call(c)
		items := []Adapter{
			{Name: "WLAN", Description: "Wireless Network Adapter", Status: "Up", Hardware: true, HardwareKnown: true, HasRoute: true},
			{Name: "以太网", Description: "Ethernet Adapter", Status: "Disconnected", Hardware: true, HardwareKnown: true},
			{Name: "代理 TUN", Description: "Virtual Adapter", Status: "Up", HardwareKnown: true},
		}
		activeScan = &adapterScanJob{token: 7654, cancel: func() {}, result: make(chan adapterScanResult, 1)}
		activeScan.result <- adapterScanResult{adapters: items}
		finishNICScan(7654)
		if selected() != -1 || enabledForTest(nicConfirmButton) {
			t.Fatal("新版列表自动选择或允许提交")
		}
		chooserProcFn(c, wmCommand, chooseShowAll, nil)
		for i, a := range items {
			buf := make([]uint16, 2048)
			sendMessage.Call(nicList, 0x0189, uintptr(i), uintptr(unsafe.Pointer(&buf[0])))
			label := syscall.UTF16ToString(buf)
			if !strings.Contains(label, a.Name) || !strings.Contains(label, adapterKind(a)) || !strings.Contains(label, adapterState(a)) {
				t.Fatal("自绘列表丢失原生可读取文字", label)
			}
		}
		for _, dpi := range []int32{96, 120, 144, 192} {
			r := nativeRect{0, 0, scale(740, dpi), scale(580, dpi)}
			handleDPI(&chooserView, uintptr(dpi)|uintptr(dpi)<<16, unsafe.Pointer(&r))
			chooserView.layoutChooser()
			height, _, _ := sendMessage.Call(nicList, 0x01a1, 0, 0)
			if int32(height) != scale(48, dpi) {
				t.Fatal("网卡行高没有随 DPI 更新", dpi, height)
			}
		}
		// 走原生键盘选择和父窗口通知，保持完整的手动确认流程。
		sendMessage.Call(nicList, 0x0100, 0x28, 0)
		if selected() != 0 || !enabledForTest(nicConfirmButton) || !strings.Contains(nativeTextForTest(nicInfo), "Wireless Network Adapter") {
			t.Fatal("键盘选择或详情同步失败")
		}
		if !strings.Contains(nicRowDetails(items[0]), "物理网卡 · 默认路由") || !strings.Contains(nicRowDetails(items[2]), "虚拟网卡") {
			t.Fatal("行摘要的硬件与路由属性错误")
		}
	})
}

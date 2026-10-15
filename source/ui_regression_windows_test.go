//go:build windows

package main

import (
	"context"
	"strings"
	"testing"
	"time"
	"unsafe"
)

func enabledForTest(h uintptr) bool { r, _, _ := u32.NewProc("IsWindowEnabled").Call(h); return r != 0 }
func visibleStyleForTest(h uintptr) bool {
	r, _, _ := u32.NewProc("GetWindowLongPtrW").Call(h, ^uintptr(15))
	return r&wVisible != 0
}

func TestUIRepairMethodsAreIndependent(t *testing.T) {
	preventLiveDiscovery(t)
	runOnGUIThread(func() {
		dllPath = ""
		yamlPath = ""
		h := createMainWindow()
		defer destroyWindow.Call(h)
		if currentMode != modeNone || visibleStyleForTest(dllText) || visibleStyleForTest(yamlText) {
			t.Fatal("启动时自动选定了修复方式")
		}
		mainCommand(chooseDLLMode)
		if !visibleStyleForTest(dllText) || visibleStyleForTest(yamlText) || enabledForTest(renameButton) {
			t.Fatal("DLL 模式显示或前提错误")
		}
		dllPath = `C:\OPPO\daemon\wintun.dll`
		refreshMainActions()
		if enabledForTest(renameButton) {
			t.Fatal("未确认手机连接就允许修复")
		}
		sendMessage.Call(checkBox, 0x00f1, 1, 0)
		mainCommand(checkDLLReady)
		if !enabledForTest(renameButton) {
			t.Fatal("前提确认后仍不能操作")
		}
		mainCommand(chooseYAMLMode)
		if visibleStyleForTest(dllText) || !visibleStyleForTest(yamlText) || enabledForTest(openChooserButton) {
			t.Fatal("YAML 模式错误")
		}
		mainCommand(renameDll) // 另一方式的命令不应打开确认窗或执行文件操作。
		yamlPath = `C:\merge.yaml`
		refreshMainActions()
		if !enabledForTest(openChooserButton) {
			t.Fatal("选定配置后仍不能选卡")
		}
		mainCommand(chooseDLLMode)
		if enabledForTest(renameButton) {
			t.Fatal("切换返回后仍沿用上次连接勾选")
		}
		dllPath = `C:\OPPO\daemon\wintun.dll.oppo互联`
		refreshMainActions()
		if !enabledForTest(restoreDLLButton) {
			t.Fatal("恢复错误地依赖手机连接勾选")
		}
	})
}
func TestUICancelBackgroundScanAndIgnoreOldResult(t *testing.T) {
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		previous := adapterScanner
		defer func() { adapterScanner = previous }()
		started, cancelled := make(chan struct{}), make(chan struct{})
		adapterScanner = func(ctx context.Context) ([]Adapter, error) {
			close(started)
			<-ctx.Done()
			close(cancelled)
			return nil, ctx.Err()
		}
		c := createChooserWindow()
		nicScan()
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("扫描未在后台启动")
		}
		if enabledForTest(nicConfirmButton) || selected() != -1 {
			t.Fatal("扫描期间可以提交")
		}
		destroyWindow.Call(c)
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("关闭窗口没有取消扫描")
		}
		if activeScan != nil || chooser != 0 {
			t.Fatal("取消后仍保留扫描状态")
		}
		c = createChooserWindow()
		defer destroyWindow.Call(c)
		activeScan = &adapterScanJob{token: 9002, cancel: func() {}, result: make(chan adapterScanResult, 1)}
		activeScan.result <- adapterScanResult{adapters: []Adapter{{Name: "Wi-Fi", Hardware: true, HardwareKnown: true}}}
		finishNICScan(9001)
		if activeScan == nil || len(adapters) != 0 {
			t.Fatal("旧窗口结果被使用")
		}
		finishNICScan(9002)
		if len(adapters) != 1 || selected() != -1 || enabledForTest(nicConfirmButton) {
			t.Fatal("扫描结果默认选择了网卡")
		}
		sendMessage.Call(nicList, 0x0186, 0, 0)
		nicDetails()
		if !enabledForTest(nicConfirmButton) {
			t.Fatal("手选后没有允许最终确认")
		}
	})
}
func TestUISmallWindowCanScrollToActions(t *testing.T) {
	preventLiveDiscovery(t)
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		setRepairMode(modeYAML)
		moveWindowProc.Call(h, 0, 0, uintptr(scale(720, mainView.dpi)), uintptr(scale(360, mainView.dpi)), 0)
		mainView.scroll = contentHeight
		mainView.layoutMain()
		_, height := mainView.size()
		if mainView.scroll <= 0 {
			t.Fatal("小窗口没有滚动")
		}
		var r nativeRect
		u32.NewProc("GetWindowRect").Call(openChooserButton, uintptr(unsafe.Pointer(&r)))
		u32.NewProc("MapWindowPoints").Call(0, h, uintptr(unsafe.Pointer(&r)), 2)
		if r.Top < 0 || r.Bottom > scale(height, mainView.dpi) {
			t.Fatal("滚动到底仍不能看到修复按钮", r, height)
		}
	})
}
func TestAdapterUnknownInformationAndExactName(t *testing.T) {
	a, err := parseAdapters([]byte(`{"Adapters":[{"Name":" Wi-Fi ","IfIndex":2}],"Routes":[],"RoutesKnown":false}`))
	if err != nil {
		t.Fatal(err)
	}
	if a[0].Name != " Wi-Fi " || a[0].HardwareKnown || a[0].RouteKnown || !strings.Contains(adapterKind(a[0]), "未能读取") {
		t.Fatal("名称被裁剪或未知属性被当作确定值", a)
	}
	out, err := yamlEdit([]byte("tun:\n  enable: true\n"), a[0].Name)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `interface-name: " Wi-Fi "`) {
		t.Fatal("原始接口名没有精确保留")
	}
}

//go:build windows

package main

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"
)

// 通过窗口所有权、调度切换及消息队列行为验证线程亲和性，不检查源码字符串。
func TestGUIThreadAffinity(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	getThreadID := k32.NewProc("GetCurrentThreadId")
	windowThread := u32.NewProc("GetWindowThreadProcessId")
	postMessage := u32.NewProc("PostMessageW")
	peekMessage := u32.NewProc("PeekMessageW")
	runOnGUIThread(func() {
		owner, _, _ := getThreadID.Call()
		class, _ := syscall.UTF16PtrFromString("STATIC")
		title, _ := syscall.UTF16PtrFromString("oppo-repair-thread-regression-hidden")
		inst, _, _ := getModule.Call(0)
		hwnd, _, err := createWindow.Call(0, uintptr(unsafe.Pointer(class)), uintptr(unsafe.Pointer(title)), 0, 0, 0, 10, 10, 0, 0, inst, 0)
		if hwnd == 0 {
			t.Fatalf("隐藏窗口创建失败：%v", err)
		}
		defer func() {
			current, _, _ := getThreadID.Call()
			if current != owner {
				t.Errorf("销毁窗口阶段 GUI 从线程 %d 迁移到 %d", owner, current)
			}
			if ok, _, err := destroyWindow.Call(hwnd); ok == 0 {
				t.Errorf("不能在所属线程关闭窗口：%v", err)
			} else {
				t.Logf("窗口在所属线程 %d 正常销毁", current)
			}
		}()
		actualOwner, _, _ := windowThread.Call(hwnd, 0)
		if actualOwner != owner {
			t.Fatal("窗口所有者不是 GUI 线程")
		}
		started, release, stopped := make(chan struct{}), make(chan struct{}), make(chan struct{})
		go func() {
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			defer close(stopped)
			close(started)
			<-release
		}()
		defer func() { close(release); <-stopped }()
		<-started
		for i := 0; i < 25; i++ {
			runtime.Gosched()
			runtime.GC()
			current, _, _ := getThreadID.Call()
			if current != owner {
				t.Fatalf("GUI 从线程 %d 迁移到 %d", owner, current)
			}
		}
		const probeMessage = 0x8001
		if ok, _, err := postMessage.Call(hwnd, probeMessage, 42, 0); ok == 0 {
			t.Fatalf("发送窗口消息失败：%v", err)
		}
		var event message
		ok, _, _ := peekMessage.Call(uintptr(unsafe.Pointer(&event)), hwnd, probeMessage, probeMessage, 1)
		if ok == 0 || event.Hwnd != hwnd || event.WParam != 42 {
			t.Fatal("GUI 线程没有收到自己窗口的消息")
		}
		dispatchMessage.Call(uintptr(unsafe.Pointer(&event)))
		t.Logf("25 次调度/GC 后仍在窗口所属线程 %d，消息接收通过", owner)
	})
}

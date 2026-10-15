//go:build windows

package main

import "syscall"

// Windows 在启动时读取内嵌 manifest。这里也保留源码直接运行和旧系统的回退。
func configureDPIAwareness() {
	setContext := u32.NewProc("SetProcessDpiAwarenessContext")
	if setContext.Find() == nil {
		setContext.Call(^uintptr(3)) // DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2
	} else {
		setAwareness := syscall.NewLazyDLL("shcore.dll").NewProc("SetProcessDpiAwareness")
		if setAwareness.Find() == nil {
			setAwareness.Call(2) // PROCESS_PER_MONITOR_DPI_AWARE
		} else {
			u32.NewProc("SetProcessDPIAware").Call()
		}
	}
}

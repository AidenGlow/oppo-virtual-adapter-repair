//go:build windows

package main

import (
	"context"
	"testing"
	"time"
)

func preventLiveDiscovery(t *testing.T) {
	t.Helper()
	previous := pathDiscoverer
	pathDiscoverer = func(context.Context, repairMode) pathSearchResult { return pathSearchResult{} }
	t.Cleanup(func() { pathDiscoverer = previous })
}
func awaitPathResult(t *testing.T, job *pathSearchJob) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for len(job.result) == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(job.result) == 0 {
		t.Fatal("自动识别没有返回")
	}
	finishPathSearch(job.token)
}
func TestUIAutoLocateDLLDoesNotConfirmRepair(t *testing.T) {
	previous := pathDiscoverer
	defer func() { pathDiscoverer = previous }()
	pathDiscoverer = func(context.Context, repairMode) pathSearchResult {
		return pathSearchResult{Candidates: []pathCandidate{{Path: `C:\OPPO\daemon\wintun.dll`, Label: "已找到 OPPO 文件"}}}
	}
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		setRepairMode(modeDLL)
		awaitPathResult(t, activePathSearch)
		if dllPath == "" || enabledForTest(renameButton) {
			t.Fatal("自动定位未完成或跳过了手机连接确认")
		}
		check, _, _ := sendMessage.Call(checkBox, checkGet, 0, 0)
		if check != 0 {
			t.Fatal("自动勾选了手机连接")
		}
	})
}
func TestUIBothProxyClientsRemainUserSelectable(t *testing.T) {
	previous := pathDiscoverer
	defer func() { pathDiscoverer = previous }()
	pathDiscoverer = func(context.Context, repairMode) pathSearchResult {
		return pathSearchResult{Candidates: []pathCandidate{{Path: `C:\Rev\config.yaml`, Label: "Clash Verge Rev", Client: clientRev}, {Path: `C:\CFW\profiles\current.yml`, Label: "Clash for Windows", Client: clientCFW}}}
	}
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		setRepairMode(modeYAML)
		awaitPathResult(t, activePathSearch)
		if yamlPath != "" || enabledForTest(openChooserButton) {
			t.Fatal("两个客户端都存在时擅自选择")
		}
		for i, kind := range []string{clientRev, clientCFW} {
			sendMessage.Call(yamlText, comboSetSelection, uintptr(i+1), 0)
			useProxyChoice()
			if selectedProxy == nil || selectedProxy.Client != kind || !enabledForTest(openChooserButton) {
				t.Fatal("无法自主选择客户端", kind)
			}
		}
	})
}
func TestUIPathSearchCancellationAndStaleResult(t *testing.T) {
	previous := pathDiscoverer
	defer func() { pathDiscoverer = previous }()
	started, cancelled := make(chan struct{}), make(chan struct{})
	pathDiscoverer = func(ctx context.Context, mode repairMode) pathSearchResult {
		if mode == modeDLL {
			close(started)
			<-ctx.Done()
			close(cancelled)
		}
		return pathSearchResult{}
	}
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		setRepairMode(modeDLL)
		oldToken := activePathSearch.token
		select {
		case <-started:
		case <-time.After(time.Second):
			t.Fatal("查找没有启动")
		}
		setRepairMode(modeYAML)
		newToken := activePathSearch.token
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("切换方式没有取消原查找")
		}
		finishPathSearch(oldToken)
		if activePathSearch == nil || activePathSearch.token != newToken || dllPath != "" {
			t.Fatal("过期查找结果覆盖了当前界面")
		}
	})
}
func TestProxyProcessNamesAreCaseInsensitive(t *testing.T) {
	for _, sample := range []struct{ name, kind string }{{"CLASH-VERGE.EXE", clientRev}, {"Clash for Windows.exe", clientCFW}, {"Clash.for.Windows.EXE", clientCFW}} {
		if !proxyProcessMatches(sample.name, sample.kind) {
			t.Fatal("无法识别客户端进程", sample)
		}
	}
	if proxyProcessMatches("mihomo.exe", clientRev) {
		t.Fatal("把独立内核误当成 GUI 客户端")
	}
}

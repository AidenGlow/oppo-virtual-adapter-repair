//go:build windows

package main

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestUINICSimplifiedViewDoesNotReuseSelection(t *testing.T) {
	preventLiveDiscovery(t)
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		c := createChooserWindow()
		defer destroyWindow.Call(c)
		allAdapters = markRecommendedAdapters([]Adapter{
			{Name: "WLAN", HardwareKnown: true, Hardware: true, Status: "Up", HasRoute: true, RouteKnown: true, Gateway: "192.168.1.1"},
			{Name: "TUN", HardwareKnown: true, Status: "Up"},
			{Name: "Ethernet", HardwareKnown: true, Hardware: true, Status: "Down"},
		})
		fillNICList()
		if len(adapters) != 1 || !adapters[0].Recommended || selected() != -1 || enabledForTest(nicConfirmButton) {
			t.Fatal("simple view or initial selection incorrect", adapters)
		}
		sendMessage.Call(nicList, 0x0186, 0, 0)
		nicDetails()
		if !strings.Contains(nativeTextForTest(nicInfo), "建议选择") || !enabledForTest(nicConfirmButton) {
			t.Fatal("recommendation reason missing")
		}
		chooserProcFn(c, wmCommand, chooseShowAll, nil)
		if len(adapters) != 3 || selected() != -1 || enabledForTest(nicConfirmButton) {
			t.Fatal("filter reused selection")
		}
		sendMessage.Call(nicList, 0x0186, 1, 0)
		nicDetails()
		if selected() != 1 || !strings.Contains(nativeTextForTest(nicInfo), "TUN") {
			t.Fatal("filtered row identity mismatch")
		}
		chooserProcFn(c, wmCommand, chooseShowAll, nil)
		if len(adapters) != 1 || selected() != -1 || enabledForTest(nicConfirmButton) {
			t.Fatal("hidden selection remained active")
		}
		saveWindowForTest(t, c, "ui-nic-simple.png")
		allAdapters = []Adapter{{Name: "TUN", Status: "Up"}}
		fillNICList()
		if len(adapters) != 0 || !strings.Contains(nativeTextForTest(nicStatus), "显示全部") {
			t.Fatal("empty state has no escape")
		}
		saveWindowForTest(t, c, "ui-nic-empty.png")
	})
}
func TestUIEffectivenessRunsInBackgroundAndRejectsStaleResults(t *testing.T) {
	preventLiveDiscovery(t)
	previous := effectivenessChecker
	defer func() { effectivenessChecker = previous }()
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		setRepairMode(modeYAML)
		stopPathSearch()
		useManualProxy(`C:\fixture\config.yaml`)
		cancelled := make(chan struct{})
		effectivenessChecker = func(ctx context.Context, _ pathCandidate, _ string) effectivenessResult {
			<-ctx.Done()
			close(cancelled)
			return effectivenessResult{"unknown", "cancelled", ""}
		}
		beginEffectivenessCheck()
		job := activeEffectivenessCheck
		if job == nil || enabledForTest(checkEffectiveButton) {
			t.Fatal("check not running in background")
		}
		setRepairMode(modeDLL)
		select {
		case <-cancelled:
		case <-time.After(time.Second):
			t.Fatal("mode switch did not cancel check")
		}
		finishEffectivenessCheck(job.token)
		if currentMode != modeDLL || activeEffectivenessCheck != nil {
			t.Fatal("old result changed repair mode")
		}
		setRepairMode(modeYAML)
		stopPathSearch()
		useManualProxy(`C:\fixture\next.yaml`)
		effectivenessChecker = func(context.Context, pathCandidate, string) effectivenessResult {
			return effectivenessResult{"effective", "配置已生效", "测试样本"}
		}
		beginEffectivenessCheck()
		job = activeEffectivenessCheck
		deadline := time.Now().Add(time.Second)
		for len(job.result) == 0 && time.Now().Before(deadline) {
			time.Sleep(time.Millisecond)
		}
		finishEffectivenessCheck(job.token + 100)
		if activeEffectivenessCheck == nil {
			t.Fatal("stale token accepted")
		}
		finishEffectivenessCheck(job.token)
		if activeEffectivenessCheck != nil || !enabledForTest(checkEffectiveButton) || !strings.Contains(nativeTextForTest(statusText), "配置已生效") {
			t.Fatal("completion state incorrect")
		}
		if currentMode != modeYAML || enabledForTest(renameButton) {
			t.Fatal("check changed independent repair methods")
		}
		saveWindowForTest(t, h, "ui-check-result.png")
	})
}

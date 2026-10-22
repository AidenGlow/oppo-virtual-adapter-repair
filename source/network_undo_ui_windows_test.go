//go:build windows

package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"unsafe"
)

func TestUINetworkUndoRequiresSelectionConfirmationAndKeepsFullRestore(t *testing.T) {
	preventLiveDiscovery(t)
	target, backup := undoFixture(t, "mixed-port: 7890\ntun:\n  enable: true\nproxies:\n  - name: original\nrules:\n  - MATCH,DIRECT\n")
	current, _ := os.ReadFile(target)
	current = bytes.ReplaceAll(current, []byte("name: original"), []byte("name: later"))
	current = bytes.ReplaceAll(current, []byte("MATCH,DIRECT"), []byte("MATCH,REJECT"))
	os.WriteFile(target, current, 0600)
	before := backupDirectorySnapshotForTest(t, filepath.Dir(target))
	oldConfirm, oldNotice, oldRunning := confirmNetworkUndo, networkUndoNotice, proxyClientRunning
	defer func() { confirmNetworkUndo, networkUndoNotice, proxyClientRunning = oldConfirm, oldNotice, oldRunning }()
	confirmations := 0
	confirmNetworkUndo = func(owner uintptr, plan *yamlNetworkUndoPlan) bool {
		confirmations++
		if plan.restore.targetPath != target || plan.restore.backupPath != backup || !strings.Contains(plan.summary, "interface-name") {
			t.Fatal("incorrect confirmation", plan)
		}
		return false
	}
	networkUndoNotice = func(uintptr, string, string, uintptr) bool { return false }
	proxyClientRunning = func(string) bool { return false }
	runOnGUIThread(func() {
		main := createMainWindow()
		defer destroyWindow.Call(main)
		setRepairMode(modeYAML)
		stopPathSearch()
		useManualProxy(target)
		window := createNetworkUndoWindow(target)
		defer func() {
			if backupWindow != 0 {
				destroyWindow.Call(backupWindow)
			}
		}()
		if !backupUndoMode || len(backupEntries) != 1 || selectedYAMLBackup() != -1 || enabledForTest(backupConfirm) {
			t.Fatal("undo auto-selected or missing record")
		}
		sendMessage.Call(backupList, 0x0186, 0, 0)
		showYAMLBackupDetails()
		if !enabledForTest(backupConfirm) || !strings.Contains(nativeTextForTest(backupDetailsText), "interface-name") {
			t.Fatal("no field preview")
		}
		backupCommand(backupConfirmID)
		if confirmations != 1 || backupWindow != window {
			t.Fatal("final denial not respected")
		}
		if after := backupDirectorySnapshotForTest(t, filepath.Dir(target)); !reflect.DeepEqual(before, after) {
			t.Fatal("denial wrote files")
		}
		saveWindowForTest(t, window, "ui-network-undo-preview.png")
		confirmNetworkUndo = func(uintptr, *yamlNetworkUndoPlan) bool { return true }
		backupCommand(backupConfirmID)
		if backupWindow != 0 {
			t.Fatal("successful undo not closed")
		}
		out, _ := os.ReadFile(target)
		if !bytes.Contains(out, []byte("name: later")) || !bytes.Contains(out, []byte("MATCH,REJECT")) || bytes.Contains(out, []byte("interface-name")) {
			t.Fatal(string(out))
		}
		createYAMLBackupWindow(target)
		if backupUndoMode || len(backupEntries) < 2 {
			t.Fatal("full restore disappeared after undo")
		}
		saveWindowForTest(t, main, "ui-network-actions.png")
	})
}
func TestUINetworkUndoLegacyAndConflictStatesAreReadOnly(t *testing.T) {
	preventLiveDiscovery(t)
	target, backup := undoFixture(t, "mixed-port: 7890\ntun:\n  enable: true\n")
	fixed, _ := os.ReadFile(target)
	os.WriteFile(target, bytes.ReplaceAll(fixed, []byte(`"WLAN"`), []byte(`"Other"`)), 0600)
	before := backupDirectorySnapshotForTest(t, filepath.Dir(target))
	runOnGUIThread(func() {
		main := createMainWindow()
		defer destroyWindow.Call(main)
		h := createNetworkUndoWindow(target)
		sendMessage.Call(backupList, 0x0186, 0, 0)
		showYAMLBackupDetails()
		if enabledForTest(backupConfirm) || !strings.Contains(nativeTextForTest(backupDetailsText), "不一致") {
			t.Fatal("conflict could overwrite later network choice")
		}
		destroyWindow.Call(h)
	})
	if after := backupDirectorySnapshotForTest(t, filepath.Dir(target)); !reflect.DeepEqual(before, after) {
		t.Fatal("conflict preview changed files")
	}
	os.Remove(backup + networkReceiptSuffix)
	runOnGUIThread(func() {
		main := createMainWindow()
		defer destroyWindow.Call(main)
		h := createNetworkUndoWindow(target)
		defer destroyWindow.Call(h)
		if len(backupEntries) != 0 || enabledForTest(backupConfirm) || !strings.Contains(nativeTextForTest(backupStatus), "旧版备份") {
			t.Fatal("legacy empty state unclear")
		}
	})
}
func TestUIFourNetworkActionsFitMinimumWidthAtAllDPI(t *testing.T) {
	preventLiveDiscovery(t)
	runOnGUIThread(func() {
		h := createMainWindow()
		defer destroyWindow.Call(h)
		setRepairMode(modeYAML)
		stopPathSearch()
		for _, dpi := range []int32{96, 120, 144, 192} {
			r := nativeRect{0, 0, scale(600, dpi), scale(524, dpi)}
			handleDPI(&mainView, uintptr(dpi)|uintptr(dpi)<<16, unsafe.Pointer(&r))
			mainView.layoutMain()
			var client nativeRect
			getClientRectProc.Call(h, uintptr(unsafe.Pointer(&client)))
			right := int32(0)
			count := 0
			for _, c := range mainView.controls {
				if strings.HasPrefix(c.key, "yamlAction") {
					var bounds nativeRect
					u32.NewProc("GetWindowRect").Call(c.hwnd, uintptr(unsafe.Pointer(&bounds)))
					u32.NewProc("MapWindowPoints").Call(0, h, uintptr(unsafe.Pointer(&bounds)), 2)
					if bounds.Left < right || bounds.Right > client.Right || bounds.Top < 0 || bounds.Bottom > client.Bottom {
						t.Fatal("network actions overlap or clip", dpi, c.key, bounds, client)
					}
					right = bounds.Right
					count++
				}
			}
			if count != 4 {
				t.Fatal("missing independent actions", count)
			}
		}
	})
}

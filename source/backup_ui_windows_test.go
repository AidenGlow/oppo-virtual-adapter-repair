//go:build windows

package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func backupDirectorySnapshotForTest(t *testing.T, dir string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		files[entry.Name()] = data
	}
	return files
}

func backupListTextForTest(t *testing.T, index int) string {
	t.Helper()
	length, _, _ := sendMessage.Call(backupList, 0x018a, uintptr(index), 0)
	if int32(length) < 0 || length > 32767 {
		t.Fatal("无法读取备份列表原生文字长度", length)
	}
	buf := make([]uint16, int(length)+1)
	if n, _, _ := sendMessage.Call(backupList, 0x0189, uintptr(index), uintptr(unsafe.Pointer(&buf[0]))); int32(n) < 0 {
		t.Fatal("无法读取备份列表原生文字")
	}
	return syscall.UTF16ToString(buf)
}

func TestUIBackupListSelectionRefreshCancelAndDPI(t *testing.T) {
	preventLiveDiscovery(t)
	target, oldBackup := restoreFixture(t, []byte("port: 7891\n"), []byte("port: 7890\n"))
	newBackup := target + ".oppo-backup-20261016-010203.004.bak"
	backupDiscoveryFile(t, newBackup, []byte("port: 7892\n"))
	before := backupDirectorySnapshotForTest(t, filepath.Dir(target))
	runOnGUIThread(func() {
		main := createMainWindow()
		defer destroyWindow.Call(main)
		h := createYAMLBackupWindow(target)
		defer func() {
			if backupWindow != 0 {
				destroyWindow.Call(backupWindow)
			}
		}()
		count, _, _ := sendMessage.Call(backupList, 0x018b, 0, 0)
		if count != 2 || len(backupEntries) != 2 {
			t.Fatal("没有自动列出当前配置的两份备份", count, backupEntries)
		}
		for i, path := range []string{newBackup, oldBackup} {
			label := backupListTextForTest(t, i)
			if !strings.Contains(label, path) || !strings.Contains(label, backupEntries[i].Time.Format("2006-01-02 15:04:05.000")) {
				t.Fatal("原生列表文字丢失路径、时间或倒序", label)
			}
		}
		if selectedYAMLBackup() != -1 || enabledForTest(backupConfirm) {
			t.Fatal("自动查找后默认选择了备份或允许提交")
		}
		// 使用原生键盘选择和父窗口通知验证详情及确认按钮同步。
		sendMessage.Call(backupList, 0x0100, 0x28, 0)
		if selectedYAMLBackup() != 0 || !enabledForTest(backupConfirm) || !strings.Contains(nativeTextForTest(backupDetailsText), newBackup) {
			t.Fatal("手动选择备份后详情或按钮未同步")
		}
		for _, dpi := range []int32{96, 120, 144, 192} {
			r := nativeRect{0, 0, scale(740, dpi), scale(500, dpi)}
			handleDPI(&backupView, uintptr(dpi)|uintptr(dpi)<<16, unsafe.Pointer(&r))
			layoutBackupChooser()
			height, _, _ := sendMessage.Call(backupList, 0x01a1, 0, 0)
			if int32(height) != scale(48, dpi) {
				t.Fatal("备份列表行高没有随 DPI 更新", dpi, height)
			}
			var client nativeRect
			getClientRectProc.Call(h, uintptr(unsafe.Pointer(&client)))
			previousRight := int32(0)
			for _, c := range backupView.controls {
				if c.key != "manual" && c.key != "refresh" && c.key != "confirm" && c.key != "cancel" {
					continue
				}
				var bounds nativeRect
				u32.NewProc("GetWindowRect").Call(c.hwnd, uintptr(unsafe.Pointer(&bounds)))
				u32.NewProc("MapWindowPoints").Call(0, h, uintptr(unsafe.Pointer(&bounds)), 2)
				if bounds.Left < previousRight || bounds.Right > client.Right || bounds.Top < 0 || bounds.Bottom > client.Bottom {
					t.Fatal("备份操作按钮在 DPI 缩放后重叠或超出窗口", dpi, c.key, bounds)
				}
				previousRight = bounds.Right
			}
		}
		saveWindowForTest(t, h, "ui-backups-sample.png")
		sendMessage.Call(h, wmCommand, backupRefreshID, 0)
		if selectedYAMLBackup() != -1 || enabledForTest(backupConfirm) || !strings.Contains(nativeTextForTest(backupDetailsText), "尚未选择备份") {
			t.Fatal("刷新后错误保留了选择、详情或提交权限")
		}
		sendMessage.Call(h, wmCommand, backupCancelID, 0)
		if backupWindow != 0 {
			t.Fatal("取消没有关闭备份窗口")
		}
	})
	if after := backupDirectorySnapshotForTest(t, filepath.Dir(target)); !reflect.DeepEqual(after, before) {
		t.Fatal("打开、选择、刷新或取消备份窗口修改了文件")
	}
}

func TestUIBackupEmptyStatesKeepManualSelection(t *testing.T) {
	preventLiveDiscovery(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "config.yaml")
	backupDiscoveryFile(t, target, []byte("port: 7890\n"))
	before := backupDirectorySnapshotForTest(t, dir)
	runOnGUIThread(func() {
		main := createMainWindow()
		defer destroyWindow.Call(main)
		for _, current := range []string{"", filepath.Join(dir, "missing.yaml"), target} {
			h := createYAMLBackupWindow(current)
			count, _, _ := sendMessage.Call(backupList, 0x018b, 0, 0)
			manual, _, _ := u32.NewProc("GetDlgItem").Call(h, backupManualID)
			if count != 0 || selectedYAMLBackup() != -1 || enabledForTest(backupConfirm) || !enabledForTest(manual) {
				t.Fatal("无配置或无备份时状态错误，或手动入口不可用", current)
			}
			if nativeTextForTest(backupStatus) == "" {
				t.Fatal("备份空状态缺少文字说明", current)
			}
			destroyWindow.Call(h)
		}
	})
	if after := backupDirectorySnapshotForTest(t, dir); !reflect.DeepEqual(after, before) {
		t.Fatal("备份空状态产生了文件副作用")
	}
}

func TestUIBackupListAndManualRequireSameFinalConfirmation(t *testing.T) {
	preventLiveDiscovery(t)
	target, backup := restoreFixture(t, []byte("port: 7891\n"), []byte("port: 7890\n"))
	before := backupDirectorySnapshotForTest(t, filepath.Dir(target))
	previousPicker, previousConfirm := chooseYAMLBackupFile, confirmYAMLRestore
	defer func() { chooseYAMLBackupFile, confirmYAMLRestore = previousPicker, previousConfirm }()
	var plans []*yamlRestorePlan
	var owners []uintptr
	confirmYAMLRestore = func(owner uintptr, plan *yamlRestorePlan) bool {
		owners = append(owners, owner)
		plans = append(plans, plan)
		return false
	}
	runOnGUIThread(func() {
		main := createMainWindow()
		defer destroyWindow.Call(main)
		h := createYAMLBackupWindow(target)
		defer destroyWindow.Call(h)
		sendMessage.Call(backupList, 0x0100, 0x28, 0)
		sendMessage.Call(h, wmCommand, backupConfirmID, 0)
		if len(plans) != 1 || backupWindow != h {
			t.Fatal("列表选择没有进入最终确认，或否决后错误关闭窗口")
		}
		pickerOwner := uintptr(0)
		chooseYAMLBackupFile = func(owner uintptr) string {
			pickerOwner = owner
			return backup
		}
		sendMessage.Call(h, wmCommand, backupManualID, 0)
		if len(plans) != 2 || pickerOwner != h || backupWindow != h {
			t.Fatal("手动选择没有进入相同的最终确认流程")
		}
		for i, plan := range plans {
			if owners[i] != h || plan.backupPath != backup || plan.targetPath != target || string(plan.backupData) != "port: 7891\n" || string(plan.targetData) != "port: 7890\n" {
				t.Fatal("最终确认没有收到相同的已校验路径和快照", i, plan)
			}
		}
		chooseYAMLBackupFile = func(uintptr) string { return "" }
		sendMessage.Call(h, wmCommand, backupManualID, 0)
		if len(plans) != 2 || backupWindow != h {
			t.Fatal("手动选文件取消后仍进入确认或关闭窗口")
		}
	})
	if after := backupDirectorySnapshotForTest(t, filepath.Dir(target)); !reflect.DeepEqual(after, before) {
		t.Fatal("最终确认否决或手动取消仍写入配置或建立保护备份")
	}
}

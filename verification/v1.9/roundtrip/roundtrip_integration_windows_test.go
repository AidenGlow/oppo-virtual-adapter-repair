//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// 只操作本测试线程、精确 owner 和标题的消息框；最终确认前核对临时目标路径。
func driveTemporaryDialogs(owner uintptr, thread uintptr, confirmation, completion, target string) <-chan error {
	done := make(chan error, 1)
	go func() {
		confirmed, completed := false, false
		var problem error
		callback := syscall.NewCallback(func(h, _ uintptr) uintptr {
			parent, _, _ := u32.NewProc("GetWindow").Call(h, 4)
			if parent != owner {
				return 1
			}
			class := make([]uint16, 32)
			u32.NewProc("GetClassNameW").Call(h, uintptr(unsafe.Pointer(&class[0])), uintptr(len(class)))
			if syscall.UTF16ToString(class) != "#32770" {
				return 1
			}
			title := nativeTextForTest(h)
			if title == confirmation && !confirmed {
				bodyControl, _, _ := u32.NewProc("GetDlgItem").Call(h, 0xffff)
				body := nativeTextForTest(bodyControl)
				if !strings.Contains(body, target) {
					problem = fmt.Errorf("确认框没有临时配置完整路径：%q", body)
					postMessageProc.Call(h, wmCommand, 7, 0) // IDNO
					return 0
				}
				confirmed = true
				postMessageProc.Call(h, wmCommand, yes, 0)
			} else if title == completion && confirmed && !completed {
				completed = true
				postMessageProc.Call(h, wmCommand, 1, 0)
			} else if title != confirmation && title != completion {
				problem = fmt.Errorf("临时流程出现非预期消息框：%s", title)
				postMessageProc.Call(h, wmCommand, 1, 0)
			}
			return 1
		})
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			u32.NewProc("EnumThreadWindows").Call(thread, callback, 0)
			if problem != nil {
				done <- problem
				return
			}
			if confirmed && completed {
				done <- nil
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		done <- fmt.Errorf("测试消息框未在限定时间内完成：confirmed=%v completed=%v", confirmed, completed)
	}()
	return done
}

func TestTemporaryYAMLRepairAutoFindRestore(t *testing.T) {
	preventLiveDiscovery(t)
	dir := t.TempDir()
	target := filepath.Join(dir, "仅测试用配置.yaml")
	original := []byte("\xef\xbb\xbf# 临时配置：保留中文、BOM、CRLF 和注释\r\nport: 7890\r\ninterface-name: 'Ethernet' # 原始网卡\r\ntun:\r\n  enable: true\r\n  auto-detect-interface: true\r\nrules:\r\n  - MATCH,DIRECT # 不相关字段\r\n")
	if err := os.WriteFile(target, original, 0600); err != nil {
		t.Fatal(err)
	}
	read := func(path string) []byte {
		t.Helper()
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	var repaired, restored, originalBackup, protectionBackup []byte
	var backupPath, protectionPath string
	runOnGUIThread(func() {
		main := createMainWindow()
		defer destroyWindow.Call(main)
		currentMode = modeYAML
		mainView.layoutMain()
		useManualProxy(target)
		thread, _, _ := k32.NewProc("GetCurrentThreadId").Call()
		picker := createChooserWindow()
		defer func() {
			if chooser != 0 {
				destroyWindow.Call(chooser)
			}
			if backupWindow != 0 {
				destroyWindow.Call(backupWindow)
			}
		}()
		// 注入一张测试网卡，不调用 Windows 网卡扫描或读取真实客户端。
		adapters = []Adapter{{Name: "Temporary-Test-Wi-Fi", Description: "仅用于临时测试", Status: "Up", Hardware: true, HardwareKnown: true}}
		row := pstr("Temporary-Test-Wi-Fi | 临时测试网卡")
		sendMessage.Call(nicList, listAdd, 0, uintptr(unsafe.Pointer(row)))
		sendMessage.Call(nicList, 0x0186, 0, 0)
		nicDetails()
		dialogs := driveTemporaryDialogs(picker, thread, "最终确认：修复配置", "修改完成", target)
		sendMessage.Call(picker, wmCommand, chooseConfirm, 0)
		if err := <-dialogs; err != nil {
			t.Fatal(err)
		}
		if chooser != 0 {
			t.Fatal("修复成功后网卡窗口没有关闭")
		}
		repaired = read(target)
		if bytes.Equal(repaired, original) || !bytes.Contains(repaired, []byte(`interface-name: "Temporary-Test-Wi-Fi"`)) || !bytes.Contains(repaired, []byte("auto-detect-interface: false")) {
			t.Fatal("修复没有真实改变临时配置中的预期字段")
		}
		if !bytes.HasPrefix(repaired, []byte{0xef, 0xbb, 0xbf}) || bytes.Count(repaired, []byte("\n")) != bytes.Count(repaired, []byte("\r\n")) {
			t.Fatal("修复丢失 BOM 或改变 CRLF")
		}
		beforeList := backupDirectorySnapshotForTest(t, dir)
		window := createYAMLBackupWindow(target) // 真正的自动备份发现及原生列表。
		if len(backupEntries) != 1 || selectedYAMLBackup() != -1 || enabledForTest(backupConfirm) {
			t.Fatal("自动备份列表数量或未选择状态不正确")
		}
		backupPath = backupEntries[0].Path // 取自自动列表，不预先指定修复返回值。
		originalBackup = read(backupPath)
		if !bytes.Equal(originalBackup, original) {
			t.Fatal("自动找到的备份不是原始文件字节")
		}
		sendMessage.Call(backupList, 0x0100, 0x28, 0)
		if selectedYAMLBackup() != 0 || !enabledForTest(backupConfirm) || !strings.Contains(nativeTextForTest(backupDetailsText), backupPath) {
			t.Fatal("真实原生列表选择没有同步到恢复目标")
		}
		if afterList := backupDirectorySnapshotForTest(t, dir); !reflect.DeepEqual(beforeList, afterList) {
			t.Fatal("打开或选择备份列表写入了文件")
		}
		saveWindowForTest(t, window, "roundtrip-backup-selected.png")
		dialogs = driveTemporaryDialogs(window, thread, "最终确认：恢复 YAML", "恢复完成", target)
		sendMessage.Call(window, wmCommand, backupConfirmID, 0)
		if err := <-dialogs; err != nil {
			t.Fatal(err)
		}
		if backupWindow != 0 {
			t.Fatal("恢复成功后备份窗口没有关闭")
		}
		restored = read(target)
		if !bytes.Equal(restored, original) {
			t.Fatal("恢复后的原文件逐字节不一致")
		}
		if !bytes.Equal(read(backupPath), original) {
			t.Fatal("原始备份被改动")
		}
		all, err := findYAMLBackups(target)
		if err != nil || len(all) != 2 {
			t.Fatal("恢复后没有保留原始备份及当前配置保护备份", err)
		}
		for _, backup := range all {
			if backup.Path != backupPath {
				protectionPath = backup.Path
			}
		}
		protectionBackup = read(protectionPath)
		if !bytes.Equal(protectionBackup, repaired) {
			t.Fatal("恢复前的保护备份不等于修复后配置")
		}
		if files, err := os.ReadDir(dir); err != nil || len(files) != 3 {
			t.Fatal("临时目录出现非预期文件或残留锁 / 写入临时文件", err)
		}
	})
	summary := map[string]any{
		"Version": appVersion, "Result": "PASS", "IsolatedTemporaryFilesOnly": true,
		"ProductionDiscoveryDisabled": true, "NetworkAdapterScanDisabled": true,
		"NativeRepairConfirmationYes": true, "NativeBackupListSelection": true, "NativeRestoreConfirmationYes": true,
		"AutoFoundBackupsBeforeRestore": 1, "BackupsAfterRestore": 2,
		"RepairChangedExpectedFields": true, "DiscoveryAndSelectionReadOnly": true,
		"OriginalBackupMatchesOriginal": true, "RestoredBytesEqualOriginal": true,
		"ProtectionBackupMatchesRepaired": true, "OriginalBackupUnchanged": true,
		"OriginalSHA256": fmt.Sprintf("%x", sha256.Sum256(original)),
		"RepairedSHA256": fmt.Sprintf("%x", sha256.Sum256(repaired)),
		"RestoredSHA256": fmt.Sprintf("%x", sha256.Sum256(restored)),
		"OriginalBytes":  len(original), "RepairedBytes": len(repaired), "RestoredBytes": len(restored),
	}
	if dir := os.Getenv("OPPO_REPAIR_ROUNDTRIP_EVIDENCE_DIR"); dir != "" {
		for name, data := range map[string][]byte{"original.yaml": original, "repaired.yaml": repaired, "restored.yaml": restored, "original-backup.bak": originalBackup, "protection-backup.bak": protectionBackup} {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0600); err != nil {
				t.Fatal(err)
			}
		}
		data, _ := json.MarshalIndent(summary, "", "  ")
		if err := os.WriteFile(filepath.Join(dir, "result.json"), append(data, '\n'), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("原始 SHA256=%x；恢复后 SHA256=%x；保护备份与修复后内容一致", sha256.Sum256(original), sha256.Sum256(restored))
}

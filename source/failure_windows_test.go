//go:build windows

package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestFileFailureMessagesClassifyWindowsErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		code syscall.Errno
		mode repairMode
		want string
		step string
	}{
		{"permission", 5, modeYAML, "权限不足或访问受限", "属性 → 安全"},
		{"dll_access_denied_is_ambiguous", 5, modeDLL, "权限不足或访问受限", "不能排除 DLL 被加载"},
		{"privilege", 1314, modeYAML, "权限不足或访问受限", "管理员授权"},
		{"elevation", 740, modeYAML, "权限不足或访问受限", "管理员授权"},
		{"read_only", 19, modeYAML, "权限不足或访问受限", "只读状态"},
		{"network_permission", 65, modeYAML, "权限不足或访问受限", "属性 → 安全"},
		{"sharing_dll", 32, modeDLL, "文件被占用", "登录和手机连接前提"},
		{"locking_yaml", 33, modeYAML, "文件被占用", "包括托盘程序"},
		{"missing_file", 2, modeDLL, "路径已失效或不可用", "重新查找"},
		{"missing_directory", 3, modeYAML, "路径已失效或不可用", "重新识别"},
		{"missing_drive", 15, modeDLL, "路径已失效或不可用", "手动选择"},
		{"network_path", 53, modeYAML, "路径已失效或不可用", "手动指定"},
		{"network_name", 67, modeYAML, "路径已失效或不可用", "手动指定"},
		{"invalid_name", 123, modeYAML, "路径已失效或不可用", ".bak"},
		{"bad_path", 161, modeDLL, "路径已失效或不可用", "wintun.dll.oppo互联"},
		{"long_path", 206, modeYAML, "路径已失效或不可用", "手动指定"},
		{"not_a_directory", 267, modeYAML, "路径已失效或不可用", "重新识别"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "实际失败文件.yaml")
			err := fmt.Errorf("已建立备份，但操作失败：%w", &os.PathError{Op: "test", Path: path, Err: tc.code})
			message := fileFailureMessage(err, tc.mode, "不应显示的备用路径")
			for _, want := range []string{tc.want, tc.step, "下一步：", path, fmt.Sprintf("Windows 错误码：%d", tc.code), err.Error()} {
				if !strings.Contains(message, want) {
					t.Fatalf("缺少 %q：%s", want, message)
				}
			}
			if strings.Contains(message, "不应显示的备用路径") {
				t.Fatal("没有使用原始错误中的实际失败路径")
			}
		})
	}
	link := &os.LinkError{Op: "备份", Old: "原配置.yaml", New: "保护备份.bak", Err: syscall.Errno(32)}
	message := fileFailureMessage(link, modeYAML, "")
	if !strings.Contains(message, "原配置.yaml\n→\n保护备份.bak") {
		t.Fatal("没有同时显示源路径和目标路径", message)
	}
}

func TestFileFailureMessagesPreserveSafetyErrors(t *testing.T) {
	for _, err := range []error{
		errors.New("目标文件已存在，为防止覆盖已取消"),
		errors.New("备份与编辑快照不一致，已停止；未写入配置"),
		errors.New("YAML 存在重复键或无效值"),
		syscall.Errno(112), // 磁盘满不能误报为权限、占用或路径问题。
	} {
		if message := fileFailureMessage(err, modeYAML, "merge.yaml"); message != err.Error() {
			t.Fatalf("改变了非本次范围内的提示：%s", message)
		}
	}
	if fileFailureMessage(nil, modeDLL, "") != "" {
		t.Fatal("无错误时生成了失败提示")
	}
}

func TestFileFailureMessagesRealWindowsFailures(t *testing.T) {
	t.Run("locked_dll", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), "daemon")
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "wintun.dll")
		original := testDLLImage()
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		handle, err := syscall.CreateFile(nativePath(path), syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
		if err != nil {
			t.Fatal(err)
		}
		defer syscall.CloseHandle(handle)
		_, err = changeDLL(path, false)
		if !errors.Is(err, syscall.Errno(32)) {
			t.Fatalf("没有产生预期的 Windows 共享冲突：%v", err)
		}
		message := fileFailureMessage(err, modeDLL, path)
		if !strings.HasPrefix(message, "文件被占用") || !strings.Contains(message, "手机连接前提") {
			t.Fatal(message)
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil || !bytes.Equal(data, original) {
			t.Fatal("失败后 DLL 内容发生变化", readErr)
		}
		if _, err := os.Stat(filepath.Join(dir, suffix)); !os.IsNotExist(err) {
			t.Fatal("失败后出现了改名文件", err)
		}
	})
	t.Run("permission_denied_yaml", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "merge.yaml")
		original := []byte("tun:\n  enable: true\n")
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		security, err := readFileDACL(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := applyFileDACL(path, security); err != nil {
				t.Error("还原临时文件权限失败", err)
			}
		})
		account, err := user.Current()
		if err != nil {
			t.Fatal(err)
		}
		sddl, _ := syscall.UTF16PtrFromString("D:P(D;;GR;;;" + account.Uid + ")(A;;FA;;;" + account.Uid + ")")
		var descriptor uintptr
		r, _, err := fileSecurity.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW").Call(uintptr(unsafe.Pointer(sddl)), 1, uintptr(unsafe.Pointer(&descriptor)), 0)
		runtime.KeepAlive(sddl)
		if r == 0 {
			t.Fatal(err)
		}
		defer fileKernel.NewProc("LocalFree").Call(descriptor)
		p := nativePath(path)
		r, _, err = fileSecurity.NewProc("SetFileSecurityW").Call(uintptr(unsafe.Pointer(p)), 0x80000004, descriptor)
		runtime.KeepAlive(p)
		if r == 0 {
			t.Fatal(err)
		}
		_, err = updateYaml(path, "Wi-Fi")
		if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			t.Fatalf("没有产生预期的访问拒绝：%v", err)
		}
		if message := fileFailureMessage(err, modeYAML, path); !strings.HasPrefix(message, "权限不足或访问受限") {
			t.Fatal(message)
		}
		if err := applyFileDACL(path, security); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(data, original) {
			t.Fatal("失败后 YAML 内容发生变化", err)
		}
		entries, err := os.ReadDir(filepath.Dir(path))
		if err != nil || len(entries) != 1 {
			t.Fatal("权限失败留下了备份或锁文件", err)
		}
	})
	t.Run("missing_dll", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "daemon", "wintun.dll")
		_, err := changeDLL(path, false)
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal("没有产生路径不存在错误", err)
		}
		if message := fileFailureMessage(err, modeDLL, path); !strings.HasPrefix(message, "路径已失效或不可用") || !strings.Contains(message, "重新查找") {
			t.Fatal(message)
		}
	})
}

func TestUIBackupMissingPathShowsRecoveryGuidance(t *testing.T) {
	preventLiveDiscovery(t)
	target := filepath.Join(t.TempDir(), "已移走.yaml")
	runOnGUIThread(func() {
		main := createMainWindow()
		defer destroyWindow.Call(main)
		h := createYAMLBackupWindow(target)
		defer destroyWindow.Call(h)
		message := nativeTextForTest(backupDetailsText)
		for _, want := range []string{"路径已失效或不可用", "重新识别", "手动指定", target} {
			if !strings.Contains(message, want) {
				t.Fatalf("原生备份窗口缺少 %q：%s", want, message)
			}
		}
		if enabledForTest(backupConfirm) || selectedYAMLBackup() != -1 {
			t.Fatal("路径失效后允许恢复")
		}
	})
	if entries, err := os.ReadDir(filepath.Dir(target)); err != nil || len(entries) != 0 {
		t.Fatal("只读失败提示产生了文件", err)
	}
}

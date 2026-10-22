//go:build windows

package main

import (
	"bytes"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"unsafe"
)

func TestYAMLGuardBlocksWritersAndDetectsReplacement(t *testing.T) {
	path := filepath.Join(t.TempDir(), "merge.yaml")
	data := []byte("tun:\n  enable: true\n")
	os.WriteFile(path, data, 0600)
	g, err := openYAMLGuard(path)
	if err != nil {
		t.Fatal(err)
	}
	defer g.Close()
	info, _ := g.source.Stat()
	if f, err := os.OpenFile(path, os.O_WRONLY, 0); err == nil {
		f.Close()
		t.Fatal("保护期间外部可以打开写句柄")
	}
	replacement := path + ".replacement"
	os.WriteFile(replacement, data, 0600)
	g.closeSource() // 模拟提交前释放读句柄后，外部替换了同名文件。
	if err := os.Rename(replacement, path); err != nil {
		t.Fatal(err)
	}
	if err := checkFileSnapshot(path, info, data); err == nil {
		t.Fatal("相同内容的文件身份替换没有被发现")
	}
}
func TestDLLRejectsParentJunction(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "other-software")
	os.Mkdir(target, 0700)
	actual := filepath.Join(target, "wintun.dll")
	os.WriteFile(actual, testDLLImage(), 0600)
	link := filepath.Join(dir, "daemon")
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "New-Item -ItemType Junction -Path $env:OPPO_TEST_LINK -Target $env:OPPO_TEST_TARGET -ErrorAction Stop | Out-Null")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	cmd.Env = append(os.Environ(), "OPPO_TEST_LINK="+link, "OPPO_TEST_TARGET="+target)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("无法建立隔离联接测试：%v %s", err, out)
	}
	if _, err := changeDLL(filepath.Join(link, "wintun.dll"), false); err == nil {
		t.Fatal("通过目录联接修改了另一目录")
	}
	if _, err := os.Stat(actual); err != nil {
		t.Fatal("真实文件被改名", err)
	}
	os.Remove(link)
}
func fileDACLForTest(t *testing.T, path string) string {
	t.Helper()
	api := syscall.NewLazyDLL("advapi32.dll")
	p := nativePath(path)
	var size uint32
	api.NewProc("GetFileSecurityW").Call(uintptr(unsafe.Pointer(p)), 4, 0, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		t.Fatal("无法读取安全描述符大小")
	}
	descriptor := make([]byte, size)
	r, _, err := api.NewProc("GetFileSecurityW").Call(uintptr(unsafe.Pointer(p)), 4, uintptr(unsafe.Pointer(&descriptor[0])), uintptr(size), uintptr(unsafe.Pointer(&size)))
	runtime.KeepAlive(p)
	if r == 0 {
		t.Fatal(err)
	}
	var output *uint16
	var length uint32
	r, _, err = api.NewProc("ConvertSecurityDescriptorToStringSecurityDescriptorW").Call(uintptr(unsafe.Pointer(&descriptor[0])), 1, 4, uintptr(unsafe.Pointer(&output)), uintptr(unsafe.Pointer(&length)))
	if r == 0 {
		t.Fatal(err)
	}
	defer fileKernel.NewProc("LocalFree").Call(uintptr(unsafe.Pointer(output)))
	value := syscall.UTF16ToString(unsafe.Slice(output, length))
	// Windows 复制会补上“自动继承已处理”标志；比较保护状态和全部 ACE。
	return strings.Replace(value, "D:PAI(", "D:P(", 1)
}
func TestYAMLBackupAndReplacementPreserveDACL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "merge.yaml")
	original := []byte("tun:\n  enable: true\n")
	os.WriteFile(path, original, 0600)
	account, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	sddl, _ := syscall.UTF16PtrFromString("D:P(A;;FA;;;" + account.Uid + ")")
	api := syscall.NewLazyDLL("advapi32.dll")
	var descriptor uintptr
	r, _, err := api.NewProc("ConvertStringSecurityDescriptorToSecurityDescriptorW").Call(uintptr(unsafe.Pointer(sddl)), 1, uintptr(unsafe.Pointer(&descriptor)), 0)
	runtime.KeepAlive(sddl)
	if r == 0 {
		t.Fatal(err)
	}
	defer fileKernel.NewProc("LocalFree").Call(descriptor)
	p := nativePath(path)
	r, _, err = api.NewProc("SetFileSecurityW").Call(uintptr(unsafe.Pointer(p)), 0x80000004, descriptor)
	runtime.KeepAlive(p)
	if r == 0 {
		t.Fatal(err)
	}
	before := fileDACLForTest(t, path)
	backup, err := updateYaml(path, "Wi-Fi")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, backup, backup + networkReceiptSuffix} {
		if after := fileDACLForTest(t, p); after != before {
			t.Fatalf("%s 的 DACL 被改变：%s -> %s", p, before, after)
		}
	}
	_, safety, err := restoreYaml(backup)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{path, safety} {
		if after := fileDACLForTest(t, p); after != before {
			t.Fatalf("恢复 DACL 被改变：%s -> %s", before, after)
		}
	}
	data, _ := os.ReadFile(path)
	if !bytes.Equal(data, original) {
		t.Fatal("恢复内容不同")
	}
}

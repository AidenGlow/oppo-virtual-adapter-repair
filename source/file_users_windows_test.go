//go:build windows

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestFileLockHelper(t *testing.T) {
	path := os.Getenv("OPPO_REPAIR_FILE_LOCK_HELPER")
	if path == "" {
		return
	}
	handle, err := syscall.CreateFile(nativePath(path), syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(handle)
	fmt.Println("OPPO_TEST_LOCK_READY")
	io.Copy(io.Discard, os.Stdin)
}
func TestRestartManagerFindsRealTemporaryFileOwner(t *testing.T) {
	if unsafe.Sizeof(rmProcessInfo{}) != 668 {
		t.Fatal("incorrect Windows RM struct layout", unsafe.Sizeof(rmProcessInfo{}))
	}
	path := filepath.Join(t.TempDir(), "locked.yaml")
	if err := os.WriteFile(path, []byte("mixed-port: 7890\n"), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestFileLockHelper$")
	cmd.Env = append(os.Environ(), "OPPO_REPAIR_FILE_LOCK_HELPER="+path)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	input, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err = cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		input.Close()
		done := make(chan struct{})
		go func() { cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			cmd.Process.Kill()
			<-done
		}
	})
	ready := make(chan string, 1)
	go func() { line, _ := bufio.NewReader(out).ReadString('\n'); ready <- line }()
	select {
	case line := <-ready:
		if strings.TrimSpace(line) != "OPPO_TEST_LOCK_READY" {
			t.Fatal("helper not ready", line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("helper timeout")
	}
	users, err := queryFileUsers([]string{path})
	if err != nil {
		t.Fatal("Restart Manager query failed", err)
	}
	found := false
	for _, user := range users {
		if user.PID == uint32(cmd.Process.Pid) {
			found = true
		}
	}
	if !found {
		t.Fatalf("test file holder not reported: %#v", users)
	}
	probe, probeErr := syscall.CreateFile(nativePath(path), syscall.GENERIC_WRITE, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE|syscall.FILE_SHARE_DELETE, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if probeErr == nil {
		syscall.CloseHandle(probe)
		t.Fatal("lookup released the holder's file lock")
	}
	if !errors.Is(probeErr, syscall.Errno(32)) {
		t.Fatal("cannot verify holder lock", probeErr)
	}
	raw, _ := os.ReadFile(path)
	if string(raw) != "mixed-port: 7890\n" {
		t.Fatal("lookup changed file")
	}
}
func TestOccupancyHintsPreserveClassificationAndShowProcess(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locked.yaml")
	os.WriteFile(path, []byte("mixed-port: 7890\n"), 0600)
	old := fileUserQuery
	defer func() { fileUserQuery = old }()
	fileUserQuery = func(paths []string) ([]fileUser, error) {
		if len(paths) != 1 || paths[0] != path {
			t.Error(paths)
		}
		return []fileUser{{PID: 1234, Name: "Proxy Client\nwindow", Service: "test-service"}}, nil
	}
	message := fileFailureMessage(&os.PathError{Op: "replace", Path: path, Err: syscall.Errno(32)}, modeYAML, path)
	if !strings.HasPrefix(message, "文件被占用") || !strings.Contains(message, "PID 1234") || !strings.Contains(message, "Proxy Client window") || !strings.Contains(message, "原始错误") {
		t.Fatal(message)
	}
	calls := 0
	fileUserQuery = func([]string) ([]fileUser, error) { calls++; return nil, errors.New("denied") }
	message = fileFailureMessage(syscall.Errno(5), modeYAML, path)
	if calls != 0 || !strings.HasPrefix(message, "权限不足") {
		t.Fatal("YAML permission failure was treated as occupancy", message)
	}
	message = fileFailureMessage(syscall.Errno(5), modeDLL, path)
	if calls != 1 || !strings.Contains(message, "无法读取进程信息") || !strings.HasPrefix(message, "权限不足") {
		t.Fatal(message)
	}
	fileUserQuery = func([]string) ([]fileUser, error) { return nil, nil }
	if hint := occupancyFailureHint(syscall.Errno(32), modeYAML, path); !strings.Contains(hint, "未识别到具体程序") {
		t.Fatal(hint)
	}
}
func TestOccupancyLookupTimeoutAndBusyAreBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "locked.yaml")
	os.WriteFile(path, []byte("port: 1\n"), 0600)
	old := fileUserQuery
	defer func() { fileUserQuery = old }()
	release := make(chan struct{})
	finished := make(chan struct{})
	fileUserQuery = func([]string) ([]fileUser, error) { defer close(finished); <-release; return nil, nil }
	begin := time.Now()
	hint := occupancyFailureHint(syscall.Errno(32), modeYAML, path)
	if !strings.Contains(hint, "查询超时") || time.Since(begin) > 3*time.Second {
		close(release)
		t.Fatal(hint)
	}
	if hint = occupancyFailureHint(syscall.Errno(32), modeYAML, path); !strings.Contains(hint, "尚未完成") {
		close(release)
		t.Fatal(hint)
	}
	close(release)
	<-finished
	deadline := time.Now().Add(time.Second)
	for len(fileUserSlots) != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(fileUserSlots) != 0 {
		t.Fatal("lookup slot leaked")
	}
}

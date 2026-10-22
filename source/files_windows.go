//go:build windows

package main

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

var fileKernel = syscall.NewLazyDLL("kernel32.dll")
var fileSecurity = syscall.NewLazyDLL("advapi32.dll")

type fileDACL struct {
	data      []byte
	protected bool
}

func readFileDACL(path string) (*fileDACL, error) {
	p := nativePath(path)
	var size uint32
	_, _, err := fileSecurity.NewProc("GetFileSecurityW").Call(uintptr(unsafe.Pointer(p)), 4, 0, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 {
		return nil, &os.PathError{Op: "读取文件访问权限", Path: path, Err: err}
	}
	d := make([]byte, size)
	r, _, err := fileSecurity.NewProc("GetFileSecurityW").Call(uintptr(unsafe.Pointer(p)), 4, uintptr(unsafe.Pointer(&d[0])), uintptr(size), uintptr(unsafe.Pointer(&size)))
	runtime.KeepAlive(p)
	if r == 0 {
		return nil, &os.PathError{Op: "读取文件访问权限", Path: path, Err: err}
	}
	var control uint16
	var revision uint32
	r, _, err = fileSecurity.NewProc("GetSecurityDescriptorControl").Call(uintptr(unsafe.Pointer(&d[0])), uintptr(unsafe.Pointer(&control)), uintptr(unsafe.Pointer(&revision)))
	if r == 0 {
		return nil, &os.PathError{Op: "读取文件访问权限", Path: path, Err: err}
	}
	return &fileDACL{d, control&0x1000 != 0}, nil
}
func applyFileDACL(path string, d *fileDACL) error {
	flags := uintptr(0x20000004)
	if d.protected {
		flags = 0x80000004
	}
	p := nativePath(path)
	r, _, err := fileSecurity.NewProc("SetFileSecurityW").Call(uintptr(unsafe.Pointer(p)), flags, uintptr(unsafe.Pointer(&d.data[0])))
	runtime.KeepAlive(p)
	runtime.KeepAlive(d)
	if r == 0 {
		return &os.PathError{Op: "保留原文件访问权限", Path: path, Err: err}
	}
	return nil
}

func nativePath(path string) *uint16 {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	if !strings.HasPrefix(path, `\\?\`) {
		if strings.HasPrefix(path, `\\`) {
			path = `\\?\UNC\` + strings.TrimPrefix(path, `\\`)
		} else {
			path = `\\?\` + path
		}
	}
	p, _ := syscall.UTF16PtrFromString(path)
	return p
}
func checkDLLPath(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(abs)
	current := volume + string(os.PathSeparator)
	for _, part := range strings.Split(strings.TrimPrefix(abs, current), string(os.PathSeparator)) {
		if part == "" {
			continue
		}
		current = filepath.Join(current, part)
		p := nativePath(current)
		attrs, _, e := fileKernel.NewProc("GetFileAttributesW").Call(uintptr(unsafe.Pointer(p)))
		runtime.KeepAlive(p)
		if uint32(attrs) == 0xffffffff {
			return &os.PathError{Op: "检查 DLL 路径", Path: current, Err: e}
		}
		if attrs&0x400 != 0 {
			return errors.New("DLL 路径含目录联接、符号链接或重解析点，请选择真实安装目录")
		}
	}
	return nil
}
func renameNoReplace(source, target string) error {
	a, b := nativePath(source), nativePath(target)
	r, _, err := fileKernel.NewProc("MoveFileExW").Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 8)
	runtime.KeepAlive(a)
	runtime.KeepAlive(b)
	if r == 0 {
		return &os.LinkError{Op: "重命名 DLL", Old: source, New: target, Err: err}
	}
	return nil
}
func copyNewFile(source, target string) error {
	security, err := readFileDACL(source)
	if err != nil {
		return err
	}
	a, b := nativePath(source), nativePath(target)
	r, _, err := fileKernel.NewProc("CopyFileW").Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 1)
	runtime.KeepAlive(a)
	runtime.KeepAlive(b)
	if r == 0 {
		return &os.LinkError{Op: "备份文件", Old: source, New: target, Err: err}
	}
	if err = applyFileDACL(target, security); err != nil {
		os.Remove(target)
		return err
	}
	return nil
}
func replaceNative(target, temp string) error {
	security, err := readFileDACL(target)
	if err != nil {
		return err
	}
	if err = applyFileDACL(temp, security); err != nil {
		return err
	}
	a, b := nativePath(target), nativePath(temp)
	// 不使用 IGNORE_ACL_ERRORS 或 IGNORE_MERGE_ERRORS：权限保留失败即停止。
	r, _, err := fileKernel.NewProc("ReplaceFileW").Call(uintptr(unsafe.Pointer(a)), uintptr(unsafe.Pointer(b)), 0, 0, 0, 0)
	runtime.KeepAlive(a)
	runtime.KeepAlive(b)
	if r == 0 {
		return &os.LinkError{Op: "替换文件", Old: temp, New: target, Err: err}
	}
	return nil
}

type yamlFileGuard struct{ lock, source *os.File }

func openYAMLGuard(path string) (*yamlFileGuard, error) {
	if err := ordinary(path); err != nil {
		return nil, err
	}
	lockName := path + ".oppo-repair.lock"
	name := nativePath(lockName)
	// Windows 自动删除锁文件；同一路径的本工具实例不能同时提交。
	h, _, err := fileKernel.NewProc("CreateFileW").Call(uintptr(unsafe.Pointer(name)), 0xc0000000, 0, 0, 4, 0x04000102, 0)
	runtime.KeepAlive(name)
	if h == ^uintptr(0) {
		return nil, &os.PathError{Op: "建立配置文件保护", Path: lockName, Err: err}
	}
	g := &yamlFileGuard{lock: os.NewFile(h, lockName)}
	p := nativePath(path)
	// 允许读取及替换，拒绝其他进程的新写入句柄。
	h, _, err = fileKernel.NewProc("CreateFileW").Call(uintptr(unsafe.Pointer(p)), 0x80000000, 5, 0, 3, 0x80, 0)
	runtime.KeepAlive(p)
	if h == ^uintptr(0) {
		g.Close()
		return nil, &os.PathError{Op: "读取配置文件", Path: path, Err: err}
	}
	g.source = os.NewFile(h, path)
	return g, nil
}
func (g *yamlFileGuard) closeSource() {
	if g.source != nil {
		g.source.Close()
		g.source = nil
	}
}
func (g *yamlFileGuard) Close() {
	g.closeSource()
	if g.lock != nil {
		g.lock.Close()
		g.lock = nil
	}
}

func createReceiptFile(source, target string) (*os.File, error) {
	security, err := readFileDACL(source)
	if err != nil {
		return nil, err
	}
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return nil, err
	}
	if err = applyFileDACL(target, security); err != nil {
		file.Close()
		os.Remove(target)
		return nil, err
	}
	return file, nil
}

package main

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

func testDLLImage() []byte {
	b := make([]byte, 1024)
	copy(b, "MZ")
	binary.LittleEndian.PutUint32(b[60:], 128)
	copy(b[128:], "PE\x00\x00")
	binary.LittleEndian.PutUint16(b[132:], 0x8664)
	binary.LittleEndian.PutUint16(b[134:], 1)
	binary.LittleEndian.PutUint16(b[148:], 240)
	binary.LittleEndian.PutUint16(b[150:], 0x2022)
	binary.LittleEndian.PutUint16(b[152:], 0x20b)
	binary.LittleEndian.PutUint64(b[176:], 0x180000000)
	binary.LittleEndian.PutUint32(b[184:], 4096)
	binary.LittleEndian.PutUint32(b[188:], 512)
	binary.LittleEndian.PutUint32(b[208:], 8192)
	binary.LittleEndian.PutUint32(b[212:], 512)
	binary.LittleEndian.PutUint16(b[220:], 3)
	binary.LittleEndian.PutUint32(b[260:], 16)
	copy(b[392:], ".text")
	binary.LittleEndian.PutUint32(b[400:], 1)
	binary.LittleEndian.PutUint32(b[404:], 4096)
	binary.LittleEndian.PutUint32(b[408:], 512)
	binary.LittleEndian.PutUint32(b[412:], 512)
	binary.LittleEndian.PutUint32(b[428:], 0x60000020)
	b[512] = 0xc3
	return b
}
func TestDLLRejectsInvalidPE(t *testing.T) {
	notDLL := testDLLImage()
	binary.LittleEndian.PutUint16(notDLL[150:], 0x22)
	for name, b := range map[string][]byte{"MZ-only": []byte("MZ"), "not-DLL": notDLL, "truncated-section": testDLLImage()[:512]} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "daemon")
			os.Mkdir(dir, 0700)
			path := filepath.Join(dir, "wintun.dll")
			os.WriteFile(path, b, 0600)
			if _, err := changeDLL(path, false); err == nil {
				t.Fatal("不合法 DLL 被接受")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(after, b) {
				t.Fatal("拒绝后文件变化")
			}
		})
	}
}
func TestRenameNoReplacePreservesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	a, b := filepath.Join(dir, "source"), filepath.Join(dir, "target")
	os.WriteFile(a, []byte("source"), 0600)
	os.WriteFile(b, []byte("keep-target"), 0600)
	if err := renameNoReplace(a, b); err == nil {
		t.Fatal("覆盖已存在目标")
	}
	raw, _ := os.ReadFile(b)
	if string(raw) != "keep-target" {
		t.Fatal("目标被覆盖")
	}
	if _, err := os.Stat(a); err != nil {
		t.Fatal("失败后源文件丢失")
	}
}
func TestYAMLGuardRejectsSecondOperation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "merge.yaml")
	raw := []byte("tun:\n  enable: true\n")
	os.WriteFile(path, raw, 0600)
	guard, err := openYAMLGuard(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateYaml(path, "Wi-Fi"); err == nil {
		guard.Close()
		t.Fatal("并发操作没有被拒绝")
	}
	guard.Close()
	after, _ := os.ReadFile(path)
	if !bytes.Equal(after, raw) {
		t.Fatal("文件变化")
	}
	if _, err := os.Stat(path + ".oppo-repair.lock"); !os.IsNotExist(err) {
		t.Fatal("锁文件没有清理")
	}
	if _, err := updateYaml(path, "Wi-Fi"); err != nil {
		t.Fatal("锁释放后不能重试", err)
	}
}

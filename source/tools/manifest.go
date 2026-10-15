//go:build ignore

// 将 manifest 写成 Go 链接器可直接使用的 AMD64 COFF 资源，无外部工具依赖。
// 在 source 目录运行：go run ./tools/manifest.go
package main

import (
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"os"
)

func main() {
	if err := generate(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func generate() error {
	manifest, err := os.ReadFile("app.manifest")
	if err != nil {
		return err
	}
	var document any
	if err := xml.Unmarshal(manifest, &document); err != nil {
		return fmt.Errorf("manifest XML 无效：%w", err)
	}
	// 三层目录：RT_MANIFEST(24) → CREATEPROCESS_MANIFEST_RESOURCE_ID(1) → 中性语言(0)。
	data := make([]byte, (88+len(manifest)+3)&^3)
	u16 := func(b []byte, at int, v uint16) { binary.LittleEndian.PutUint16(b[at:], v) }
	u32 := func(b []byte, at int, v uint32) { binary.LittleEndian.PutUint32(b[at:], v) }
	for _, at := range []int{0, 24, 48} {
		u16(data, at+14, 1)
	}
	u32(data, 16, 24)
	u32(data, 20, 0x80000000|24)
	u32(data, 40, 1)
	u32(data, 44, 0x80000000|48)
	u32(data, 68, 72)
	u32(data, 72, 88) // 链接器以 ADDR32NB 重定位至资源节 RVA。
	u32(data, 76, uint32(len(manifest)))
	u32(data, 80, 65001)
	copy(data[88:], manifest)

	relocations := 60 + len(data)
	symbols := relocations + 10
	object := make([]byte, symbols+18+4)
	u16(object, 0, 0x8664)
	u16(object, 2, 1)
	u32(object, 8, uint32(symbols))
	u32(object, 12, 1)
	copy(object[20:], ".rsrc")
	u32(object, 36, uint32(len(data)))
	u32(object, 40, 60)
	u32(object, 44, uint32(relocations))
	u16(object, 52, 1)
	u32(object, 56, 0x40300040) // 已初始化、只读、4 字节对齐。
	copy(object[60:], data)
	u32(object, relocations, 72)
	u16(object, relocations+8, 3) // IMAGE_REL_AMD64_ADDR32NB
	copy(object[symbols:], ".rsrc")
	u16(object, symbols+12, 1)
	object[symbols+16] = 3 // IMAGE_SYM_CLASS_STATIC
	u32(object, symbols+18, 4)
	return os.WriteFile("app_windows_amd64.syso", object, 0644)
}

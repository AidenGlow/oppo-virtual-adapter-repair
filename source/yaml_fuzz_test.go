package main

import (
	"bytes"
	"testing"
)

func FuzzYAMLEditIsSafe(f *testing.F) {
	for _, raw := range unsafeYAMLCases() {
		f.Add(raw)
	}
	for _, raw := range []string{"port: 7890\n", "tun:\n    enable: true\n", "'interface-name': Ethernet\ntun:\n  enable: true\n", "---\nport: 7890\n...\n", "notes: |\n  tun:\n    enable: true\n"} {
		f.Add([]byte(raw))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 64*1024 {
			t.Skip("限定单次模糊测试样本体积")
		}
		original := bytes.Clone(raw)
		out, err := yamlEdit(raw, "无线 网卡 #1")
		if !bytes.Equal(raw, original) {
			t.Fatal("修改了输入缓冲")
		}
		if err != nil {
			if out != nil {
				t.Fatal("拒绝输入时返回了修改结果")
			}
			return
		}
		assertOnlyInterfaceChanges(t, raw, out, "无线 网卡 #1")
	})
}

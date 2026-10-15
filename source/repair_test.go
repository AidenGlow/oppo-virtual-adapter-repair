package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestAdapterParse(t *testing.T) {
	a, e := parseAdapters([]byte(`{"Adapters":[{"Name":"Clash","Description":"TUN","Status":"Up","Hardware":false,"IfIndex":4},{"Name":"Wi-Fi","Description":"Intel","Status":"Up","Hardware":true,"IfIndex":2},{"Name":"以太网","Description":"Realtek","Status":"Down","Hardware":true,"IfIndex":3}],"Routes":[{"IfIndex":4},{"IfIndex":2}]}`))
	if e != nil {
		t.Fatal(e)
	}
	if len(a) != 3 || a[0].Name != "Wi-Fi" || !a[0].HasRoute || a[2].Hardware {
		t.Fatal(a)
	}
}
func TestDllRenameRestore(t *testing.T) {
	d := filepath.Join(t.TempDir(), "myOPPO", "daemon")
	if e := os.MkdirAll(d, 0700); e != nil {
		t.Fatal(e)
	}
	src := filepath.Join(d, "wintun.dll")
	if e := os.WriteFile(src, testDLLImage(), 0600); e != nil {
		t.Fatal(e)
	}
	renamed, e := changeDLL(src, false)
	if e != nil {
		t.Fatal(e)
	}
	if filepath.Base(renamed) != suffix {
		t.Fatal(renamed)
	}
	restored, e := changeDLL(renamed, true)
	if e != nil || restored != src {
		t.Fatal(e, restored)
	}
	if _, _, e = dllPair(filepath.Join(d, "wrong.dll")); e == nil {
		t.Fatal("wrong path accepted")
	}
	if _, _, e = dllPair(filepath.Join(d, "sub", "wintun.dll")); e == nil {
		t.Fatal("wrong folder accepted")
	}
}
func TestYamlBackupRecovery(t *testing.T) {
	d := t.TempDir()
	p := filepath.Join(d, "merge.yaml")
	orig := []byte("port: 7890\r\ninterface-name: 'Ethernet' # previous\r\ntun:\r\n  enable: true\r\n  auto-detect-interface: true\r\nrules:\r\n  - MATCH,DIRECT\r\n")
	if e := os.WriteFile(p, orig, 0600); e != nil {
		t.Fatal(e)
	}
	backup, e := updateYaml(p, "Wi-Fi")
	if e != nil {
		t.Fatal(e)
	}
	b, e := os.ReadFile(p)
	if e != nil {
		t.Fatal(e)
	}
	for _, needed := range []string{`interface-name: "Wi-Fi" # previous`, `auto-detect-interface: false`, `- MATCH,DIRECT`} {
		if !strings.Contains(string(b), needed) {
			t.Fatalf("missing %s from %s", needed, string(b))
		}
	}
	if !bytes.Contains(b, []byte("\r\n")) {
		t.Fatal("CRLF not preserved")
	}
	ret, safety, e := restoreYaml(backup)
	if e != nil || ret != p || safety == "" {
		t.Fatal(e)
	}
	b, e = os.ReadFile(p)
	if e != nil || !bytes.Equal(b, orig) {
		t.Fatal("restore mismatch")
	}
}
func TestRejectComplexYaml(t *testing.T) {
	for _, s := range []string{"tun: {enable: true}\n", "tun:\n  auto-detect-interface: true\n  auto-detect-interface: false\n", "interface-name: A\ninterface-name: B\n", ""} {
		if _, e := yamlEdit([]byte(s), "Wi-Fi"); e == nil {
			t.Fatal("should reject", s)
		}
	}
}

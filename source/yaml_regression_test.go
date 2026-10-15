package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func yamlMapping(t *testing.T, raw []byte) map[string]interface{} {
	t.Helper()
	var value map[string]interface{}
	if err := yaml.Unmarshal(raw, &value); err != nil {
		t.Fatalf("配置无法解析：%v\n%s", err, raw)
	}
	return value
}

func assertOnlyInterfaceChanges(t *testing.T, before, after []byte, adapter string) {
	t.Helper()
	expected := yamlMapping(t, before)
	expected["interface-name"] = adapter
	tun, _ := expected["tun"].(map[string]interface{})
	if tun == nil {
		tun = map[string]interface{}{}
		expected["tun"] = tun
	}
	tun["auto-detect-interface"] = false
	actual := yamlMapping(t, after)
	if !reflect.DeepEqual(expected, actual) {
		t.Fatalf("修改了目标之外的字段或写入错误类型：\nwant %#v\ngot  %#v\n%s", expected, actual, after)
	}
}

func TestYAMLRegressionIndentation(t *testing.T) {
	for _, width := range []int{1, 2, 4, 8} {
		for _, exists := range []bool{false, true} {
			t.Run(fmt.Sprintf("indent_%d_existing_%v", width, exists), func(t *testing.T) {
				indent := strings.Repeat(" ", width)
				raw := "port: 7890\ninterface-name: Ethernet\ntun:\n" + indent + "enable: true\n" + indent + "mtu: 9000\n"
				if exists {
					raw += indent + "auto-detect-interface: true\n"
				}
				raw += "rules:\n  - MATCH,DIRECT\n"
				out, err := yamlEdit([]byte(raw), "无线 网卡 #1")
				if err != nil {
					t.Fatal(err)
				}
				assertOnlyInterfaceChanges(t, []byte(raw), out, "无线 网卡 #1")
			})
		}
	}
}

func TestYAMLRegressionQuotedKeysAndDocuments(t *testing.T) {
	for name, raw := range map[string]string{
		"quoted_root":             "\"tun\":\n  enable: true\n'interface-name': Ethernet # comment\n",
		"quoted_child":            "tun:\n  enable: true\n  'auto-detect-interface': true # comment\n",
		"escaped_key":             "\"\\u0069nterface-name\": Ethernet\n",
		"missing_both":            "port: 7890\nrules: [\"MATCH,DIRECT\"]\n",
		"empty_tun":               "tun: # keep\nport: 7890\n",
		"empty_fields":            "interface-name: # name\ntun:\n  auto-detect-interface: # detection\n",
		"single_document":         "---\nport: 7890\n...\n# tail\n",
		"root_indented":           "  port: 7890\n  tun:\n    enable: true\n",
		"unrelated_block_scalar":  "notes: |\n  interface-name: embedded\n  tun:\n    auto-detect-interface: true\nport: 7890\n",
		"indentless_sequence":     "tun:\n  enable: true\n  dns-hijack:\n  - any:53\n",
		"uppercase_boolean":       "interface-name: Ethernet\ntun:\n  auto-detect-interface: FALSE\n",
		"null_and_quote_comments": "interface-name: null # keep\ntun:\n  auto-detect-interface: null # keep\nother: \"a # b\"\n",
	} {
		t.Run(name, func(t *testing.T) {
			out, err := yamlEdit([]byte(raw), "Wi-Fi")
			if err != nil {
				t.Fatal(err)
			}
			assertOnlyInterfaceChanges(t, []byte(raw), out, "Wi-Fi")
		})
	}
}

func TestYAMLRegressionPreservesText(t *testing.T) {
	for name, raw := range map[string][]byte{
		"bom_crlf":     []byte("\xef\xbb\xbf# 说明\r\nport: 7890\r\n'interface-name': 'Ethernet'  # 网卡\r\ntun:\r\n    enable: true\r\n    auto-detect-interface: true\t# 检测\r\n"),
		"mixed_eol":    []byte("# keep\r\nport: 7890\ninterface-name: Ethernet # name\r\ntun:\n  enable: true\r\n  auto-detect-interface: true # detect\n"),
		"no_final_eol": []byte("port: 7890"),
	} {
		t.Run(name, func(t *testing.T) {
			original := bytes.Clone(raw)
			out, err := yamlEdit(raw, "Wi-Fi")
			if err != nil {
				t.Fatal(err)
			}
			assertOnlyInterfaceChanges(t, raw, out, "Wi-Fi")
			if !bytes.Equal(raw, original) {
				t.Fatal("修改了传入的字节缓冲")
			}
			if bytes.HasPrefix(raw, []byte{0xef, 0xbb, 0xbf}) != bytes.HasPrefix(out, []byte{0xef, 0xbb, 0xbf}) {
				t.Fatal("BOM 改变")
			}
			if bytes.HasSuffix(raw, []byte("\n")) != bytes.HasSuffix(out, []byte("\n")) {
				t.Fatal("文件末尾换行改变")
			}
			switch name {
			case "bom_crlf":
				if !bytes.Contains(out, []byte("'interface-name': \"Wi-Fi\"  # 网卡\r\n")) || !bytes.Contains(out, []byte("false\t# 检测\r\n")) {
					t.Fatal("引号、注释或 CRLF 改变")
				}
			case "mixed_eol":
				if !bytes.Contains(out, []byte("port: 7890\n")) || !bytes.Contains(out, []byte("  enable: true\r\n")) {
					t.Fatal("未修改行的换行改变")
				}
			}
		})
	}
}

func unsafeYAMLCases() map[string][]byte {
	return map[string][]byte{
		"duplicate_root":     []byte("interface-name: A\n'interface-name': B\n"),
		"duplicate_tun":      []byte("tun:\n  enable: true\n\"tun\":\n  enable: false\n"),
		"duplicate_child":    []byte("tun:\n  auto-detect-interface: true\n  'auto-detect-interface': false\n"),
		"duplicate_other":    []byte("dns:\n  enable: true\n  enable: false\n"),
		"block_interface":    []byte("interface-name: |\n  Ethernet\ntun:\n  enable: true\n"),
		"block_detection":    []byte("tun:\n  auto-detect-interface: |\n    true\n"),
		"multiline_plain":    []byte("interface-name: Ethernet\n  continued\n"),
		"multiline_quoted":   []byte("interface-name: \"Ethernet\n  continued\"\n"),
		"anchor_alias":       []byte("interface-name: &out Ethernet\nother: *out\n"),
		"alias_tun":          []byte("defaults: &d\n  enable: true\ntun: *d\n"),
		"merge":              []byte("defaults: &d\n  port: 7890\n<<: *d\n"),
		"custom_tag":         []byte("other: !custom text\n"),
		"flow_tun":           []byte("tun: {enable: true}\n"),
		"flow_root":          []byte("{port: 7890}\n"),
		"root_sequence":      []byte("- item\n- another\n"),
		"root_scalar":        []byte("scalar\n"),
		"invalid_syntax":     []byte("rules: [\n"),
		"tun_sequence":       []byte("tun:\n  - enable: true\n"),
		"interface_sequence": []byte("interface-name: [Ethernet]\n"),
		"detection_string":   []byte("tun:\n  auto-detect-interface: 'true'\n"),
		"multiple_documents": []byte("port: 7890\n---\nport: 7891\n"),
		"complex_key":        []byte("? [a, b]\n: value\n"),
		"empty":              []byte(""),
		"only_comments":      []byte("# only a comment\n"),
		"invalid_utf8":       {0xff, '\n'},
		"bare_cr":            []byte("port: 7890\rtun:\r"),
	}
}

func TestYAMLRegressionUnsafeInputsDoNotWrite(t *testing.T) {
	for name, raw := range unsafeYAMLCases() {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "merge.yaml")
			if err := os.WriteFile(path, raw, 0600); err != nil {
				t.Fatal(err)
			}
			backup, err := updateYaml(path, "Wi-Fi")
			if err == nil {
				t.Fatal("危险输入被接受")
			}
			actual, readErr := os.ReadFile(path)
			files, listErr := os.ReadDir(dir)
			if backup != "" || readErr != nil || listErr != nil || !bytes.Equal(actual, raw) || len(files) != 1 {
				t.Fatal("拒绝输入后仍有文件副作用")
			}
		})
	}
}

func TestYAMLRegressionNoopAndNames(t *testing.T) {
	raw := []byte("interface-name: Wi-Fi # original\ntun:\n  auto-detect-interface: false\n")
	out, err := yamlEdit(raw, "Wi-Fi")
	if err != nil || !bytes.Equal(out, raw) {
		t.Fatalf("相同语义配置不应被重新格式化：%v", err)
	}
	for _, name := range []string{"", "bad\nname", "bad\rname", "bad\x00name", string([]byte{0xff})} {
		if _, err := yamlEdit(raw, name); err == nil {
			t.Fatalf("接受无效名称 %q", name)
		}
	}
	for _, name := range []string{"无线 网卡 #1", "Ethernet's \"USB\"", "路径\\接口", " 网卡 ", "网络 😀"} {
		out, err := yamlEdit([]byte("port: 7890\n"), name)
		if err != nil {
			t.Fatal(err)
		}
		assertOnlyInterfaceChanges(t, []byte("port: 7890\n"), out, name)
	}
}

func TestRestoreRegressionRejectsNonYAMLTarget(t *testing.T) {
	for _, name := range []string{"unrelated.txt", "unrelated.exe", "unrelated.md", "merge.yaml.extra"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			target := filepath.Join(dir, name)
			backup := target + ".oppo-backup-20261015-010203.004.bak"
			original := []byte("ORIGINAL-UNRELATED")
			if err := os.WriteFile(target, original, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(backup, []byte("port: 7890\n"), 0600); err != nil {
				t.Fatal(err)
			}
			_, _, err := restoreYaml(backup)
			if err == nil {
				t.Fatal("恢复接受非 YAML 目标")
			}
			actual, _ := os.ReadFile(target)
			files, _ := os.ReadDir(dir)
			if !bytes.Equal(actual, original) || len(files) != 2 {
				t.Fatal("无关文件或目录被修改")
			}
		})
	}
}

func TestRestoreRegressionRejectsInvalidBackup(t *testing.T) {
	for name, raw := range map[string][]byte{"broken": []byte("rules: [\n"), "scalar": []byte("TEXT"), "sequence": []byte("- item\n"), "duplicate": []byte("port: 1\nport: 2\n"), "binary": {0xff}} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			p := filepath.Join(dir, "merge.yaml")
			b := p + ".oppo-backup-20261015-010203.004.bak"
			orig := []byte("port: 7890\n")
			if err := os.WriteFile(p, orig, 0600); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(b, raw, 0600); err != nil {
				t.Fatal(err)
			}
			if _, _, err := restoreYaml(b); err == nil {
				t.Fatal("接受损坏或非配置备份")
			}
			actual, _ := os.ReadFile(p)
			files, _ := os.ReadDir(dir)
			if !bytes.Equal(actual, orig) || len(files) != 2 {
				t.Fatal("拒绝恢复时产生文件副作用")
			}
		})
	}
}

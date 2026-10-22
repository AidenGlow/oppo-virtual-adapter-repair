//go:build windows

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func undoFixture(t *testing.T, original string) (string, string) {
	t.Helper()
	target := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(target, []byte(original), 0600); err != nil {
		t.Fatal(err)
	}
	backup, err := updateYaml(target, "WLAN")
	if err != nil {
		t.Fatal(err)
	}
	return target, backup
}
func TestNetworkUndoPreservesLaterNodesRulesAndFormatting(t *testing.T) {
	original := []byte("\xef\xbb\xbf# header\r\ninterface-name: 'old NIC' # keep\r\ntun:\r\n    enable: true\r\n    auto-detect-interface: true # auto\r\nproxies:\r\n  - {name: test, server: old.example, password: old-password}\r\nrules:\r\n  - MATCH,DIRECT")
	fixed, err := yamlEdit(original, "WLAN")
	if err != nil {
		t.Fatal(err)
	}
	current := bytes.ReplaceAll(fixed, []byte("old.example"), []byte("new.example"))
	current = bytes.ReplaceAll(current, []byte("old-password"), []byte("new-password"))
	current = bytes.ReplaceAll(current, []byte("MATCH,DIRECT"), []byte("DOMAIN,example.com,DIRECT\r\n  - MATCH,REJECT"))
	out, summary, err := undoNetworkYAML(current, original, "WLAN")
	if err != nil {
		t.Fatal(err)
	}
	expected := bytes.ReplaceAll(original, []byte("old.example"), []byte("new.example"))
	expected = bytes.ReplaceAll(expected, []byte("old-password"), []byte("new-password"))
	expected = bytes.ReplaceAll(expected, []byte("MATCH,DIRECT"), []byte("DOMAIN,example.com,DIRECT\r\n  - MATCH,REJECT"))
	expected = bytes.Replace(expected, []byte("'old NIC'"), []byte(`"old NIC"`), 1)
	if !bytes.Equal(out, expected) || !strings.Contains(summary, "interface-name") {
		t.Fatalf("changed unrelated bytes: %s\nwant: %s\n%v", out, expected, err)
	}
}
func TestNetworkUndoHandlesAbsentNullAndNewTunChildren(t *testing.T) {
	for _, original := range []string{
		"mixed-port: 7890\n",
		"mixed-port: 7890\ntun: # original empty\n",
		"mixed-port: 7890\ntun:\n  enable: true\n",
		"interface-name: null\nmixed-port: 7890\ntun:\n  auto-detect-interface: ~\n",
		"interface-name: 'a#b' # name\ntun:\n auto-detect-interface: TRUE\n",
	} {
		t.Run(strings.ReplaceAll(original, "\n", "/"), func(t *testing.T) {
			fixed, err := yamlEdit([]byte(original), "WLAN")
			if err != nil {
				t.Fatal(err)
			}
			out, _, err := undoNetworkYAML(fixed, []byte(original), "WLAN")
			if err != nil {
				t.Fatal(err)
			}
			expected, _ := parseYAMLDocument([]byte(original))
			actual, _ := parseYAMLDocument(out)
			if !sameYAMLValue(expected.Content[0], actual.Content[0]) {
				t.Fatal(string(out), original)
			}
		})
	}
	original := []byte("mixed-port: 7890\n")
	fixed, _ := yamlEdit(original, "WLAN")
	current := bytes.Replace(fixed, []byte("  auto-detect-interface: false"), []byte("  auto-detect-interface: false\n  mtu: 1400\n  dns-hijack:\n    - any:53"), 1)
	out, _, err := undoNetworkYAML(current, original, "WLAN")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(out, []byte("  mtu: 1400\n  dns-hijack:\n    - any:53")) || bytes.Contains(out, []byte("interface-name")) || bytes.Contains(out, []byte("auto-detect-interface")) {
		t.Fatal(string(out))
	}
}
func TestNetworkUndoOnlyTouchesFieldsActuallyChangedByTool(t *testing.T) {
	for _, tc := range []struct{ original, from, to, keep string }{
		{"interface-name: WLAN\ntun:\n  auto-detect-interface: true\n", "interface-name: WLAN", "interface-name: Ethernet", "interface-name: Ethernet"},
		{"interface-name: old\ntun:\n  auto-detect-interface: false\n", "auto-detect-interface: false", "auto-detect-interface: true", "auto-detect-interface: true"},
	} {
		fixed, err := yamlEdit([]byte(tc.original), "WLAN")
		if err != nil {
			t.Fatal(err)
		}
		current := strings.Replace(string(fixed), tc.from, tc.to, 1)
		out, _, err := undoNetworkYAML([]byte(current), []byte(tc.original), "WLAN")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(out), tc.keep) {
			t.Fatal("unmodified target field overwritten", string(out))
		}
	}
}
func TestNetworkUndoRejectsConflictingNetworkFields(t *testing.T) {
	original := []byte("mixed-port: 7890\ntun:\n  enable: true\n")
	fixed, _ := yamlEdit(original, "WLAN")
	for _, current := range []string{
		strings.Replace(string(fixed), `"WLAN"`, `"Other"`, 1),
		strings.Replace(string(fixed), "auto-detect-interface: false", "auto-detect-interface: true", 1),
		strings.Replace(string(fixed), "  auto-detect-interface: false\n", "", 1),
	} {
		if _, _, err := undoNetworkYAML([]byte(current), original, "WLAN"); err == nil {
			t.Fatal("conflict accepted", current)
		}
	}
}
func TestNetworkUndoRepairSelectUndoAndFullRestore(t *testing.T) {
	original := "mixed-port: 7890\ntun:\n  enable: true\nproxies:\n  - name: original\nrules:\n  - MATCH,DIRECT\n"
	target, backup := undoFixture(t, original)
	current, _ := os.ReadFile(target)
	current = bytes.ReplaceAll(current, []byte("name: original"), []byte("name: later"))
	current = bytes.ReplaceAll(current, []byte("MATCH,DIRECT"), []byte("MATCH,REJECT"))
	os.WriteFile(target, current, 0600)
	before := backupDirectorySnapshotForTest(t, filepath.Dir(target))
	plan, err := prepareNetworkUndo(backup)
	if err != nil {
		t.Fatal(err)
	}
	if after := backupDirectorySnapshotForTest(t, filepath.Dir(target)); !reflect.DeepEqual(before, after) {
		t.Fatal("preview wrote files")
	}
	dest, protect, err := applyNetworkUndo(plan)
	if err != nil || dest != target || protect == "" {
		t.Fatal(dest, protect, err)
	}
	saved, _ := os.ReadFile(protect)
	if !bytes.Equal(saved, current) {
		t.Fatal("protection backup not current configuration")
	}
	out, _ := os.ReadFile(target)
	if !bytes.Contains(out, []byte("name: later")) || !bytes.Contains(out, []byte("MATCH,REJECT")) || bytes.Contains(out, []byte("interface-name")) || bytes.Contains(out, []byte("auto-detect-interface")) {
		t.Fatal(string(out))
	}
	if _, err := prepareNetworkUndo(backup); err == nil {
		t.Fatal("same record could be undone twice")
	}
	if _, _, err := restoreYaml(backup); err != nil {
		t.Fatal("full restore regressed", err)
	}
	out, _ = os.ReadFile(target)
	if string(out) != original {
		t.Fatal("full restore did not restore whole original")
	}
}
func TestNetworkUndoRejectsMissingTamperedOrChangedRecords(t *testing.T) {
	for _, scenario := range []string{"missing", "bad-hash", "unknown-json", "oversized", "changed-target", "changed-backup", "changed-record", "forged-plan"} {
		t.Run(scenario, func(t *testing.T) {
			target, backup := undoFixture(t, "mixed-port: 7890\ntun:\n  enable: true\n")
			record := backup + networkReceiptSuffix
			plan, err := prepareNetworkUndo(backup)
			if err != nil {
				t.Fatal(err)
			}
			switch scenario {
			case "missing":
				os.Remove(record)
			case "bad-hash":
				data, _ := json.Marshal(networkReceipt{1, "WLAN", strings.Repeat("0", 64)})
				os.WriteFile(record, data, 0600)
			case "unknown-json":
				os.WriteFile(record, []byte(`{"format":1,"extra":true}`), 0600)
			case "oversized":
				os.WriteFile(record, bytes.Repeat([]byte("x"), 4097), 0600)
			case "changed-target":
				file, _ := os.ReadFile(target)
				os.WriteFile(target, append(file, []byte("log-level: debug\n")...), 0600)
			case "changed-backup":
				os.WriteFile(backup, []byte("mixed-port: 1\n"), 0600)
			case "changed-record":
				data, _ := os.ReadFile(record)
				os.WriteFile(record, append(data, ' '), 0600)
			case "forged-plan":
				plan.output = []byte("rules: []\n")
			}
			before := backupDirectorySnapshotForTest(t, filepath.Dir(target))
			if _, _, err := applyNetworkUndo(plan); err == nil {
				t.Fatal("unsafe plan accepted")
			}
			after := backupDirectorySnapshotForTest(t, filepath.Dir(target))
			if !reflect.DeepEqual(before, after) {
				t.Fatal("rejected undo changed files")
			}
		})
	}
}
func TestLegacyBackupRemainsAvailableForFullRestore(t *testing.T) {
	target, backup := restoreFixture(t, []byte("mixed-port: 7890\n"), []byte("mixed-port: 7891\n"))
	if _, err := prepareNetworkUndo(backup); err == nil {
		t.Fatal("guessed legacy repair metadata")
	}
	if _, _, err := restoreYaml(backup); err != nil {
		t.Fatal(err)
	}
	out, _ := os.ReadFile(target)
	if string(out) != "mixed-port: 7890\n" {
		t.Fatal(string(out))
	}
}

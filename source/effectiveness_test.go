package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func runtimeJSON(name string, auto, enabled bool) []byte {
	raw, _ := json.Marshal(map[string]any{"interface-name": name, "tun": map[string]bool{"auto-detect-interface": auto, "enable": enabled}})
	return raw
}
func TestRuntimeSettingsRequireCompletePositiveEvidence(t *testing.T) {
	for _, tc := range []struct {
		name  string
		raw   []byte
		state string
	}{
		{"active", runtimeJSON(" WLAN ", false, true), "effective"},
		{"different", runtimeJSON("Ethernet", false, true), "mismatch"},
		{"automatic", runtimeJSON(" WLAN ", true, true), "mismatch"},
		{"TUN off", runtimeJSON(" WLAN ", false, false), "tun-off"},
		{"missing interface", []byte(`{"tun":{"enable":true,"auto-detect-interface":false}}`), "unknown"},
		{"missing auto", []byte(`{"interface-name":" WLAN ","tun":{"enable":true}}`), "unknown"},
		{"missing enable", []byte(`{"interface-name":" WLAN ","tun":{"auto-detect-interface":false}}`), "unknown"},
		{"wrong type", []byte(`{"interface-name":" WLAN ","tun":{"enable":"true","auto-detect-interface":false}}`), "unknown"},
		{"invalid", []byte("not json"), "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if result := compareRuntimeSettings(tc.raw, " WLAN "); result.State != tc.state {
				t.Fatal(result)
			}
		})
	}
}
func TestControllerEndpointsAreLocalAndDoNotPermitHeaderInjection(t *testing.T) {
	for _, address := range []string{"127.0.0.1:9090", "[::1]:9090", "0.0.0.0:9090", "localhost:9090"} {
		raw := []byte("external-controller: '" + address + "'\n")
		if _, err := endpointFromYAML(raw); err != nil {
			t.Fatal(address, err)
		}
	}
	for _, raw := range []string{
		"external-controller: example.com:9090\n", "external-controller: 192.168.1.2:9090\n", "external-controller: 127.0.0.1:0\n",
		"external-controller: http://127.0.0.1:9090\n", "external-controller: 127.0.0.1:9090\nsecret: \"token\\r\\nInjected: yes\"\n",
		"external-controller-pipe: '\\\\remote\\pipe\\name'\n", "external-controller-pipe: '\\\\.\\pipe\\..\\file'\n",
	} {
		if _, err := endpointFromYAML([]byte(raw)); err == nil {
			t.Fatal("unsafe endpoint accepted", raw)
		}
	}
	if _, err := endpointFromYAML([]byte("external-controller-pipe: '\\\\.\\pipe\\oppo-test'\n")); err != nil {
		t.Fatal(err)
	}
}
func writeCheckFixture(t *testing.T, kind string) (pathCandidate, coreProcess) {
	t.Helper()
	root := t.TempDir()
	target := filepath.Join(root, "config.yaml")
	runtime := filepath.Join(root, "clash-verge.yaml")
	if kind == clientCFW {
		if err := os.Mkdir(filepath.Join(root, "profiles"), 0700); err != nil {
			t.Fatal(err)
		}
		target = filepath.Join(root, "profiles", "current.yml")
		if err := os.WriteFile(filepath.Join(root, "profiles", "list.yml"), []byte("index: 0\nfiles:\n  - time: current.yml\n"), 0600); err != nil {
			t.Fatal(err)
		}
		runtime = filepath.Join(root, "config.yaml")
	}
	for path, data := range map[string]string{target: "interface-name: WLAN\ntun:\n  auto-detect-interface: false\n", runtime: "external-controller: 127.0.0.1:9090\nsecret: test-only\n"} {
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return pathCandidate{Path: target, Client: kind}, coreProcess{PID: 123, Started: "fixture", DataDir: root, ConfigPath: runtime, Ports: []int{9090}}
}
func TestEffectivenessChecksRevAndCFWWithoutWritingFiles(t *testing.T) {
	for _, kind := range []string{clientRev, clientCFW} {
		t.Run(kind, func(t *testing.T) {
			candidate, core := writeCheckFixture(t, kind)
			source, _ := os.ReadFile(candidate.Path)
			runtime, _ := os.ReadFile(core.ConfigPath)
			for _, tc := range []struct {
				coreName string
				inspect  func(context.Context) ([]coreProcess, error)
				fetch    func(context.Context, coreProcess, runtimeEndpoint) ([]byte, error)
				want     string
			}{
				{"effective", func(context.Context) ([]coreProcess, error) { return []coreProcess{core}, nil }, func(context.Context, coreProcess, runtimeEndpoint) ([]byte, error) {
					return runtimeJSON("WLAN", false, true), nil
				}, "effective"},
				{"offline", func(context.Context) ([]coreProcess, error) { return nil, nil }, nil, "not-running"},
				{"inspection fails", func(context.Context) ([]coreProcess, error) { return nil, errors.New("denied") }, nil, "unknown"},
				{"unrelated core", func(context.Context) ([]coreProcess, error) {
					other := core
					other.DataDir = t.TempDir()
					other.ConfigPath = filepath.Join(other.DataDir, "config.yaml")
					return []coreProcess{other}, nil
				}, nil, "unknown"},
				{"multiple cores", func(context.Context) ([]coreProcess, error) { return []coreProcess{core, core}, nil }, nil, "unknown"},
				{"API unavailable", func(context.Context) ([]coreProcess, error) { return []coreProcess{core}, nil }, func(context.Context, coreProcess, runtimeEndpoint) ([]byte, error) {
					return nil, errors.New("unavailable")
				}, "unknown"},
			} {
				t.Run(tc.coreName, func(t *testing.T) {
					result := checkEffectiveness(context.Background(), candidate, "WLAN", runtimeDependencies{tc.inspect, tc.fetch})
					if result.State != tc.want {
						t.Fatal(result)
					}
					after, _ := os.ReadFile(candidate.Path)
					afterRuntime, _ := os.ReadFile(core.ConfigPath)
					if !bytes.Equal(source, after) || !bytes.Equal(runtime, afterRuntime) {
						t.Fatal("read-only check modified config")
					}
				})
			}
			entries, _ := os.ReadDir(filepath.Dir(core.ConfigPath))
			if len(entries) > 3 {
				t.Fatal("check created files")
			}
		})
	}
}
func TestEffectivenessDetectsChangesAndManualTargetBoundaries(t *testing.T) {
	candidate, core := writeCheckFixture(t, clientRev)
	deps := runtimeDependencies{func(context.Context) ([]coreProcess, error) { return []coreProcess{core}, nil }, func(context.Context, coreProcess, runtimeEndpoint) ([]byte, error) {
		return runtimeJSON("WLAN", false, true), nil
	}}
	if r := checkEffectiveness(context.Background(), candidate, "Ethernet", deps); r.State != "source-changed" {
		t.Fatal(r)
	}
	manual := candidate
	manual.Client = clientManual
	if coreMatchesCandidate(core, manual) {
		t.Fatal("manual merge file guessed as active runtime")
	}
	core.ConfigPath = candidate.Path
	if !coreMatchesCandidate(core, manual) {
		t.Fatal("exact manual runtime not recognized")
	}
	candidate, core = writeCheckFixture(t, clientRev)
	deps.Fetch = func(context.Context, coreProcess, runtimeEndpoint) ([]byte, error) {
		os.WriteFile(candidate.Path, []byte("interface-name: Other\n"), 0600)
		return runtimeJSON("WLAN", false, true), nil
	}
	if r := checkEffectiveness(context.Background(), candidate, "WLAN", deps); r.State == "effective" {
		t.Fatal("changed file accepted", r)
	}
	candidate, core = writeCheckFixture(t, clientCFW)
	deps.Fetch = func(context.Context, coreProcess, runtimeEndpoint) ([]byte, error) {
		os.WriteFile(filepath.Join(core.DataDir, "profiles", "list.yml"), []byte("index: -1\nfiles: []\n"), 0600)
		return runtimeJSON("WLAN", false, true), nil
	}
	if r := checkEffectiveness(context.Background(), candidate, "WLAN", deps); r.State == "effective" {
		t.Fatal("changed CFW profile accepted", r)
	}
}
func TestRepairCheckAndRestoreRoundTripUsesTemporaryFiles(t *testing.T) {
	candidate, core := writeCheckFixture(t, clientRev)
	original := []byte("# temporary fixture\r\nmixed-port: 7890\r\ntun:\r\n  enable: true\r\n  auto-detect-interface: true\r\n")
	if err := os.WriteFile(candidate.Path, original, 0600); err != nil {
		t.Fatal(err)
	}
	backup, err := updateYaml(candidate.Path, "WLAN")
	if err != nil {
		t.Fatal(err)
	}
	deps := runtimeDependencies{func(context.Context) ([]coreProcess, error) { return []coreProcess{core}, nil }, func(context.Context, coreProcess, runtimeEndpoint) ([]byte, error) {
		return runtimeJSON("WLAN", false, true), nil
	}}
	if r := checkEffectiveness(context.Background(), candidate, "WLAN", deps); r.State != "effective" {
		t.Fatal(r)
	}
	backups, err := findYAMLBackups(candidate.Path)
	if err != nil || len(backups) != 1 || backups[0].Path != backup {
		t.Fatal(backups, err)
	}
	if _, _, err := restoreYaml(backups[0].Path); err != nil {
		t.Fatal(err)
	}
	restored, _ := os.ReadFile(candidate.Path)
	if !bytes.Equal(restored, original) {
		t.Fatal("roundtrip did not restore original bytes")
	}
}

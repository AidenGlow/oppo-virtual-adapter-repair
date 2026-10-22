//go:build windows

package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func TestLoopbackCheckOnlyReadsAndRefusesRedirects(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != "GET" || r.URL.Path != "/configs" || r.Header.Get("Authorization") != "Bearer local-test-only" {
			t.Error("unexpected request", r.Method, r.URL.Path)
		}
		w.Write(runtimeJSON("WLAN", false, true))
	}))
	defer server.Close()
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	raw, err := fetchLoopbackConfigs(context.Background(), runtimeEndpoint{Address: strings.TrimPrefix(server.URL, "http://"), Secret: "local-test-only"})
	if err != nil || compareRuntimeSettings(raw, "WLAN").State != "effective" || requests != 1 {
		t.Fatal(err, string(raw), requests)
	}
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, server.URL+"/configs", http.StatusFound)
	}))
	defer redirect.Close()
	if _, err := fetchLoopbackConfigs(context.Background(), runtimeEndpoint{Address: strings.TrimPrefix(redirect.URL, "http://")}); err == nil || requests != 1 {
		t.Fatal("redirect followed", err, requests)
	}
	if _, err := fetchLoopbackConfigs(context.Background(), runtimeEndpoint{Address: "example.com:9090"}); err == nil {
		t.Fatal("remote controller accepted")
	}
}
func TestRuntimeControllerMustBelongToSelectedCore(t *testing.T) {
	old := runtimeInspector
	defer func() { runtimeInspector = old }()
	core := coreProcess{PID: 123, Started: "before", DataDir: `C:\Fixture`, ConfigPath: `C:\Fixture\config.yaml`, Ports: []int{9090}}
	for _, other := range []coreProcess{
		{PID: 123, Started: "before", DataDir: core.DataDir, ConfigPath: core.ConfigPath, Ports: []int{9091}},
		{PID: 999, Started: core.Started, DataDir: core.DataDir, ConfigPath: core.ConfigPath, Ports: core.Ports},
		{PID: 123, Started: "reused", DataDir: core.DataDir, ConfigPath: core.ConfigPath, Ports: core.Ports},
	} {
		runtimeInspector = func(context.Context) ([]coreProcess, error) { return []coreProcess{other}, nil }
		if coreStillOwnsEndpoint(context.Background(), core, 9090) {
			t.Fatal("unrelated process or port accepted", other)
		}
	}
	runtimeInspector = func(context.Context) ([]coreProcess, error) { return []coreProcess{core}, nil }
	if !coreStillOwnsEndpoint(context.Background(), core, 9090) {
		t.Fatal("correct owner rejected")
	}
}
func TestRuntimeInspectionParsesQuotedPathsWithoutCommandLineDisclosure(t *testing.T) {
	fixture := `function Get-CimInstance { [pscustomobject]@{Name='verge-mihomo.exe';ProcessId=123;CreationDate=[datetime]'2026-10-22';CommandLine='"C:\Program Files\Clash\verge-mihomo.exe" -d "C:\fixture 中文\rev" -f "C:\fixture 中文\rev\clash-verge.yaml"'} }
function Get-NetTCPConnection { [pscustomobject]@{OwningProcess=123;LocalPort=9090} }
`
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", fixture+inspectRuntimeCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	var result struct{ Cores []coreProcess }
	if json.Unmarshal(out, &result) != nil || len(result.Cores) != 1 {
		t.Fatal(string(out))
	}
	c := result.Cores[0]
	if c.PID != 123 || c.DataDir != `C:\fixture 中文\rev` || c.ConfigPath != `C:\fixture 中文\rev\clash-verge.yaml` || len(c.Ports) != 1 || c.Started == "" || strings.Contains(string(out), "CommandLine") {
		t.Fatal(string(out))
	}
}
func startTestControllerPipe(t *testing.T) (string, <-chan string) {
	t.Helper()
	name := fmt.Sprintf(`\\.\pipe\oppo-repair-regression-%d-%d`, os.Getpid(), time.Now().UnixNano())
	p := pstr(name)
	h, _, err := k32.NewProc("CreateNamedPipeW").Call(uintptr(unsafe.Pointer(p)), 3, 0, 1, 8192, 8192, 0, 0)
	if h == ^uintptr(0) {
		t.Fatal("create test pipe", err)
	}
	file := os.NewFile(h, name)
	t.Cleanup(func() { file.Close() })
	received := make(chan string, 1)
	go func() {
		defer file.Close()
		ok, _, err := k32.NewProc("ConnectNamedPipe").Call(h, 0)
		if ok == 0 && err != syscall.Errno(535) {
			received <- "connect failed"
			return
		}
		reader := bufio.NewReader(file)
		headers := ""
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				received <- headers
				return
			}
			headers += line
			if line == "\r\n" {
				break
			}
		}
		body := runtimeJSON("WLAN", false, true)
		fmt.Fprintf(file, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: %d\r\nConnection: close\r\n\r\n%s", len(body), body)
		received <- headers
	}()
	return name, received
}
func TestWindowsNamedPipeCheckIsReadOnlyAndChecksOwner(t *testing.T) {
	pipe, received := startTestControllerPipe(t)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	raw, err := fetchPipeConfigs(ctx, coreProcess{PID: os.Getpid()}, runtimeEndpoint{Pipe: pipe, Secret: "local-test-only"})
	if err != nil || compareRuntimeSettings(raw, "WLAN").State != "effective" {
		t.Fatal("pipe check", err, string(raw))
	}
	select {
	case headers := <-received:
		if !strings.HasPrefix(headers, "GET /configs HTTP/1.1\r\n") || !strings.Contains(headers, "Authorization: Bearer local-test-only") {
			t.Fatal(headers)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	pipe, received = startTestControllerPipe(t)
	if _, err := fetchPipeConfigs(ctx, coreProcess{PID: os.Getpid() + 1}, runtimeEndpoint{Pipe: pipe, Secret: "must-not-send"}); err == nil {
		t.Fatal("wrong pipe owner accepted")
	}
	select {
	case headers := <-received:
		if headers != "" {
			t.Fatal("request sent before checking owner", headers)
		}
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
}

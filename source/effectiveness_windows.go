//go:build windows

package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// 仅返回定位内核所需的路径、PID、启动时间和监听端口，不输出完整命令行。
const inspectRuntimeCommand = `[Console]::OutputEncoding=[System.Text.Encoding]::UTF8
$ErrorActionPreference='Stop'
function FlagValue([string]$line,[string]$flag) {
 $m=[regex]::Matches($line,'(?:^|\s)-'+$flag+'(?:=|\s+)(?:"([^\"]+)"|([^\s]+))')
 if($m.Count -ne 1){return ''}
 if($m[0].Groups[1].Success){return $m[0].Groups[1].Value}
 return $m[0].Groups[2].Value
}
$processes=@(Get-CimInstance Win32_Process -ErrorAction Stop | Where-Object {$_.Name -match '(?i)^(verge-mihomo|mihomo|clash|clash-meta|clash-win64(?:-meta|-premium)?)(\.exe)?$'})
$listeners=@()
try {$listeners=@(Get-NetTCPConnection -State Listen -ErrorAction Stop)} catch {}
$cores=@(foreach($process in $processes){
 $data=FlagValue $process.CommandLine 'd'
 $file=FlagValue $process.CommandLine 'f'
 if($data -and -not [IO.Path]::IsPathRooted($data)){$data=''}
 if($file -and -not [IO.Path]::IsPathRooted($file)) {if($data){$file=[IO.Path]::GetFullPath((Join-Path $data $file))}else{$file=''}}
 if(-not $file -and $data){$file=Join-Path $data 'config.yaml'}
 $ports=@($listeners | Where-Object {$_.OwningProcess -eq $process.ProcessId} | ForEach-Object {[int]$_.LocalPort} | Select-Object -Unique)
 [pscustomobject]@{PID=[int]$process.ProcessId;DataDir=$data;ConfigPath=$file;Started=[string]$process.CreationDate.ToUniversalTime().Ticks;Ports=$ports;ControllerOverride=[bool]($process.CommandLine -match '(?:^|\s)-(?:ext-ctl|ext-ctl-pipe|ext-ctl-unix|secret)(?:=|\s)')}
})
@{Cores=$cores}|ConvertTo-Json -Compress -Depth 5`

var runtimeInspector = inspectWindowsRuntimeProcesses

func inspectWindowsRuntimeProcesses(ctx context.Context) ([]coreProcess, error) {
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", inspectRuntimeCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("runtime inspection unavailable")
	}
	var result struct{ Cores []coreProcess }
	if json.Unmarshal(bytes.TrimPrefix(out, []byte{0xef, 0xbb, 0xbf}), &result) != nil {
		return nil, errors.New("runtime inspection invalid")
	}
	return result.Cores, nil
}
func checkWindowsEffectiveness(ctx context.Context, candidate pathCandidate, expected string) effectivenessResult {
	return checkEffectiveness(ctx, candidate, expected, runtimeDependencies{runtimeInspector, fetchWindowsRuntime})
}
func sameCore(a, b coreProcess) bool {
	return a.PID == b.PID && a.Started != "" && a.Started == b.Started && sameConfigPath(a.ConfigPath, b.ConfigPath) && strings.EqualFold(a.DataDir, b.DataDir) && !b.ControllerOverride
}
func coreStillOwnsEndpoint(ctx context.Context, core coreProcess, port int) bool {
	cores, err := runtimeInspector(ctx)
	if err != nil {
		return false
	}
	for _, c := range cores {
		if sameCore(core, c) {
			if port == 0 {
				return true
			}
			for _, p := range c.Ports {
				if p == port {
					return true
				}
			}
		}
	}
	return false
}
func fetchWindowsRuntime(ctx context.Context, core coreProcess, e runtimeEndpoint) ([]byte, error) {
	if e.Address != "" {
		_, p, _ := net.SplitHostPort(e.Address)
		port, _ := strconv.Atoi(p)
		if port > 0 && coreStillOwnsEndpoint(ctx, core, port) {
			raw, err := fetchLoopbackConfigs(ctx, e)
			if err == nil && coreStillOwnsEndpoint(ctx, core, port) {
				return raw, nil
			}
		}
	}
	if e.Pipe != "" && coreStillOwnsEndpoint(ctx, core, 0) {
		raw, err := fetchPipeConfigs(ctx, core, e)
		if err == nil && coreStillOwnsEndpoint(ctx, core, 0) {
			return raw, nil
		}
	}
	return nil, errors.New("cannot read matching core controller")
}
func fetchLoopbackConfigs(ctx context.Context, e runtimeEndpoint) ([]byte, error) {
	// 再次约束目标为本机 IP，禁用系统代理与 HTTP 跳转；只允许 GET /configs。
	host, _, err := net.SplitHostPort(e.Address)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		return nil, errors.New("not loopback")
	}
	transport := &http.Transport{Proxy: nil, DisableKeepAlives: true, DialContext: (&net.Dialer{Timeout: 2 * time.Second}).DialContext, ResponseHeaderTimeout: 3 * time.Second}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 4 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return errors.New("redirect refused") }}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+e.Address+"/configs", nil)
	if err != nil {
		return nil, err
	}
	if e.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+e.Secret)
	}
	response, err := client.Do(req)
	if err != nil {
		return nil, errors.New("controller unavailable")
	}
	defer response.Body.Close()
	return readConfigsBody(response)
}
func readConfigsBody(response *http.Response) ([]byte, error) {
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("controller status %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (2<<20)+1))
	if err != nil || len(raw) > 2<<20 {
		return nil, errors.New("invalid controller response size")
	}
	return raw, nil
}

// 支持 Rev 的 Windows 本地管道。连接后先核对服务端 PID，之后才发送只读请求。
const pipeConfigsCommand = `[Console]::OutputEncoding=[System.Text.Encoding]::UTF8
$ErrorActionPreference='Stop'
Add-Type -TypeDefinition 'using System; using System.Runtime.InteropServices; public static class OppoPipeOwner { [DllImport("kernel32.dll",SetLastError=true)] public static extern bool GetNamedPipeServerProcessId(IntPtr h,out uint pid); }'
$pipe=$null
try {
 $pipe=[IO.Pipes.NamedPipeClientStream]::new('.',$env:OPPO_REPAIR_CHECK_PIPE,[IO.Pipes.PipeDirection]::InOut,[IO.Pipes.PipeOptions]::Asynchronous)
 $pipe.Connect(1500)
 $owner=[uint32]0
 if(-not [OppoPipeOwner]::GetNamedPipeServerProcessId($pipe.SafePipeHandle.DangerousGetHandle(),[ref]$owner) -or $owner -ne [uint32]$env:OPPO_REPAIR_CHECK_PID){throw 'owner mismatch'}
 $crlf=[string][char]13+[char]10
 $request='GET /configs HTTP/1.1'+$crlf+'Host: localhost'+$crlf+'Connection: close'+$crlf
 if($env:OPPO_REPAIR_CHECK_SECRET){$request+='Authorization: Bearer '+$env:OPPO_REPAIR_CHECK_SECRET+$crlf}
 $request+=$crlf
 $bytes=[Text.Encoding]::UTF8.GetBytes($request)
 $write=$pipe.WriteAsync($bytes,0,$bytes.Length)
 if(-not $write.Wait(1500)){throw 'write timeout'}
 $memory=[IO.MemoryStream]::new()
 $buffer=[byte[]]::new(8192)
 $timer=[Diagnostics.Stopwatch]::StartNew()
 while($true){
  if($timer.ElapsedMilliseconds -gt 5000){throw 'read timeout'}
  $read=$pipe.ReadAsync($buffer,0,$buffer.Length)
  if(-not $read.Wait(2000)){throw 'read timeout'}
  $count=$read.Result
  if($count -eq 0){break}
  $memory.Write($buffer,0,$count)
  if($memory.Length -gt 2200000){throw 'response too large'}
 }
 @{Response=[Convert]::ToBase64String($memory.ToArray())}|ConvertTo-Json -Compress
} catch { @{Error='pipe-unavailable'}|ConvertTo-Json -Compress }
finally {if($null -ne $pipe){$pipe.Dispose()}}`

func fetchPipeConfigs(ctx context.Context, core coreProcess, e runtimeEndpoint) ([]byte, error) {
	const prefix = `\\.\pipe\`
	if !strings.HasPrefix(strings.ToLower(e.Pipe), prefix) || len(e.Pipe) == len(prefix) || strings.ContainsAny(e.Pipe[len(prefix):], `/\:`+"\x00\r\n") {
		return nil, errors.New("invalid pipe")
	}
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", pipeConfigsCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(strings.ToUpper(entry), "OPPO_REPAIR_CHECK_") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "OPPO_REPAIR_CHECK_PIPE="+e.Pipe[len(prefix):], "OPPO_REPAIR_CHECK_PID="+strconv.Itoa(core.PID), "OPPO_REPAIR_CHECK_SECRET="+e.Secret)
	out, err := cmd.Output()
	if err != nil {
		return nil, errors.New("pipe unavailable")
	}
	var result struct{ Response string }
	if json.Unmarshal(bytes.TrimPrefix(out, []byte{0xef, 0xbb, 0xbf}), &result) != nil || result.Response == "" {
		return nil, errors.New("invalid pipe response")
	}
	raw, err := base64.StdEncoding.DecodeString(result.Response)
	if err != nil {
		return nil, err
	}
	response, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), nil)
	if err != nil {
		return nil, errors.New("invalid pipe HTTP response")
	}
	defer response.Body.Close()
	return readConfigsBody(response)
}

//go:build windows

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// 仅返回安装目录和进程路径，不输出命令行、订阅地址或配置内容。
const discoveryCommand = `[Console]::OutputEncoding=[System.Text.Encoding]::UTF8
$ErrorActionPreference='Stop'
$roots=[System.Collections.Generic.List[object]]::new()
function Add-Root([string]$path,[string]$kind,[string]$source) { if ($path) { $roots.Add(@{Path=$path;Kind=$kind;Source=$source}) } }
$keys=@('HKCU:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\Software\Microsoft\Windows\CurrentVersion\Uninstall\*','HKLM:\Software\WOW6432Node\Microsoft\Windows\CurrentVersion\Uninstall\*')
foreach ($app in @(Get-ItemProperty -Path $keys -ErrorAction SilentlyContinue)) {
  $kind=''
  if ($app.DisplayName -match '(?i)OPPO|Oplus|跨屏互联|PC[ -]?Connect|O\+\s*Connect') { $kind='dll' }
  elseif ($app.DisplayName -match '(?i)Clash.*Verge') { $kind='rev' }
  elseif ($app.DisplayName -match '(?i)Clash.*Windows') { $kind='cfw' }
  if (-not $kind) { continue }
  Add-Root ([string]$app.InstallLocation).Trim('"') $kind 'Windows 安装记录'
  foreach ($value in @($app.DisplayIcon,$app.UninstallString)) {
    if ($value -match '^"([^"]+\.exe)"|^(.+?\.exe)(?:,|\s|$)') {
      $exe=if($Matches[1]){$Matches[1]}else{$Matches[2]}
      Add-Root ([IO.Path]::GetDirectoryName($exe)) $kind 'Windows 安装记录'
    }
  }
}
foreach ($p in @(Get-Process -ErrorAction SilentlyContinue)) {
  $kind=''
  if ($p.ProcessName -match '(?i)^(PCConnect.*|Oppo.*|Oplus.*|MultiScreen.*|O\+\s*Connect)$') {$kind='dll'}
  elseif ($p.ProcessName -match '(?i)^clash-verge$') {$kind='rev'}
  elseif ($p.ProcessName -match '(?i)^Clash[ ._-]*for[ ._-]*Windows$') {$kind='cfw'}
  if($kind) {try {if($p.Path) {Add-Root ([IO.Path]::GetDirectoryName($p.Path)) $kind '正在运行的程序'}} catch {}}
}
try {
  foreach ($p in @(Get-CimInstance Win32_Process -ErrorAction Stop | Where-Object {$_.Name -match '(?i)^(mihomo|clash|clash-meta|clash-win64-meta|clash-verge|Clash for Windows)(\.exe)?$'})) {
    if ($p.CommandLine -match '(?:^|\s)-d(?:=|\s+)(?:"([^"]+)"|([^\s]+))') {
      $dir=if($Matches[1]){$Matches[1]}else{$Matches[2]}
      if(Test-Path -LiteralPath (Join-Path $dir 'verge.yaml')) {Add-Root $dir 'rev' '运行中的内核目录'}
      elseif(Test-Path -LiteralPath (Join-Path $dir 'profiles\list.yml')) {Add-Root $dir 'cfw' '运行中的内核目录'}
    }
  }
} catch {}
ConvertTo-Json -InputObject @($roots.ToArray()) -Compress -Depth 4`

func defaultDiscoveryRoots() []searchRoot {
	roots := []searchRoot{}
	for _, base := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs")} {
		if base == "" {
			continue
		}
		for _, name := range []string{"PC Connect", "OPPO PC Connect", "OPPO互联", "跨屏互联", `OPPO\PC Connect`, `OPlus\PC Connect`, `OPlus\CrossDevice`} {
			roots = append(roots, searchRoot{filepath.Join(base, name), "dll", "常见安装目录"})
		}
	}
	if appData := os.Getenv("APPDATA"); appData != "" {
		roots = append(roots, searchRoot{filepath.Join(appData, "io.github.clash-verge-rev.clash-verge-rev"), clientRev, "默认用户目录"})
	}
	if home := os.Getenv("USERPROFILE"); home != "" {
		roots = append(roots, searchRoot{filepath.Join(home, ".config", "clash"), clientCFW, "默认用户目录"})
	}
	if home := os.Getenv("CFW_HOME"); home != "" {
		roots = append(roots, searchRoot{home, clientCFW, "CFW_HOME"})
	}
	return roots
}
func discoverPaths(parent context.Context, mode repairMode) pathSearchResult {
	ctx, cancel := context.WithTimeout(parent, 10*time.Second)
	defer cancel()
	roots := defaultDiscoveryRoots()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", discoveryCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	raw, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return pathSearchResult{Note: "查找已取消或超时，可以重新识别或手动选择。"}
	}
	if err == nil {
		raw = bytes.TrimPrefix(bytes.TrimSpace(raw), []byte{0xef, 0xbb, 0xbf})
		var extra []searchRoot
		if json.Unmarshal(raw, &extra) == nil {
			for _, root := range extra {
				if root.Kind == "dll" {
					roots = append(roots, root)
					parent := filepath.Dir(root.Path)
					if parent != root.Path {
						roots = append(roots, searchRoot{parent, "dll", root.Source})
					}
				}
				if root.Kind == clientRev || root.Kind == clientCFW {
					roots = append(roots, root, searchRoot{filepath.Join(root.Path, "data"), root.Kind, root.Source})
					if root.Kind == clientCFW {
						roots = append(roots, searchRoot{filepath.Join(root.Path, ".config", "clash"), clientCFW, root.Source})
					}
				}
			}
		}
	}
	if mode == modeDLL {
		only := []searchRoot{}
		for _, root := range roots {
			if root.Kind == "dll" {
				only = append(only, root)
			}
		}
		return findDLLCandidates(ctx, only)
	}
	return findProxyCandidates(ctx, roots)
}
func runningProxyClient(kind string) bool {
	if kind != clientRev && kind != clientCFW {
		return false
	}
	h, err := syscall.CreateToolhelp32Snapshot(syscall.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return true
	}
	defer syscall.CloseHandle(h)
	entry := syscall.ProcessEntry32{Size: uint32(unsafe.Sizeof(syscall.ProcessEntry32{}))}
	for err = syscall.Process32First(h, &entry); err == nil; err = syscall.Process32Next(h, &entry) {
		if proxyProcessMatches(syscall.UTF16ToString(entry.ExeFile[:]), kind) {
			return true
		}
	}
	return false
}

func proxyProcessMatches(name, kind string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	if kind == clientRev {
		return name == "clash-verge"
	}
	return kind == clientCFW && strings.NewReplacer(" ", "", ".", "", "-", "", "_", "").Replace(name) == "clashforwindows"
}

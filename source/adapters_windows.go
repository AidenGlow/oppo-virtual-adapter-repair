//go:build windows

package main

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// Fixed command, no untrusted arguments. Read-only: scans all Windows-known adapters.
const scanCommand = `[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$ErrorActionPreference='Stop'
$items=@(Get-NetAdapter -IncludeHidden | ForEach-Object { [pscustomobject]@{ Name=[string]$_.Name; Description=[string]$_.InterfaceDescription; Status=[string]$_.Status; Hardware=$(if ($null -eq $_.HardwareInterface) { $null } else { [bool]$_.HardwareInterface }); IfIndex=[int]$_.ifIndex; MacAddress=[string]$_.MacAddress } })
$routesKnown=$true
try { $routes=@(Get-NetRoute -AddressFamily IPv4 -DestinationPrefix '0.0.0.0/0' -ErrorAction Stop | ForEach-Object { [pscustomobject]@{ IfIndex=[int]$_.ifIndex; Gateway=[string]$_.NextHop } }) } catch { $routes=@(); $routesKnown=$false }
@{Adapters=$items;Routes=$routes;RoutesKnown=$routesKnown} | ConvertTo-Json -Compress -Depth 5`

func scanWindowsAdapters() ([]Adapter, error) {
	return scanWindowsAdaptersContext(context.Background())
}

func scanWindowsAdaptersContext(parent context.Context) ([]Adapter, error) {
	ctx, cancel := context.WithTimeout(parent, 18*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoLogo", "-NoProfile", "-NonInteractive", "-Command", scanCommand)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, e := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return nil, fmt.Errorf("扫描超时：%w", ctx.Err())
	}
	if e != nil {
		m := strings.TrimSpace(string(out))
		if len(m) > 700 {
			m = m[:700]
		}
		return nil, fmt.Errorf("读取网卡信息失败：%v %s", e, m)
	}
	return parseAdapters(out)
}

//go:build windows

package main

import (
	"context"
	"runtime"
	"unsafe"
)

const (
	findDLL           = 105
	dllChoiceID       = 106
	proxyChoiceID     = 205
	findProxy         = 206
	wmPathsDone       = 0x8003
	comboAdd          = 0x0143
	comboReset        = 0x014b
	comboGetSelection = 0x0147
	comboSetSelection = 0x014e
)

type pathSearchJob struct {
	token  uintptr
	mode   repairMode
	cancel context.CancelFunc
	result chan pathSearchResult
}

var (
	dllFindButton, proxyFindButton uintptr
	dllCandidates, proxyCandidates []pathCandidate
	selectedProxy                  *pathCandidate
	pathSequence                   uintptr
	activePathSearch               *pathSearchJob
	searchedPaths                  [3]bool
	pathDiscoverer                 = discoverPaths
)

func fillPathChoices(control uintptr, placeholder string, candidates []pathCandidate) {
	sendMessage.Call(control, comboReset, 0, 0)
	for _, label := range append([]string{placeholder}, candidateLabels(candidates)...) {
		p := pstr(label)
		sendMessage.Call(control, comboAdd, 0, uintptr(unsafe.Pointer(p)))
		runtime.KeepAlive(p)
	}
	sendMessage.Call(control, comboSetSelection, 0, 0)
}
func candidateLabels(candidates []pathCandidate) []string {
	out := make([]string, len(candidates))
	for i, c := range candidates {
		out[i] = c.Label
	}
	return out
}
func selectedPathIndex(control uintptr) int {
	r, _, _ := sendMessage.Call(control, comboGetSelection, 0, 0)
	return int(int32(r)) - 1
}
func useDLLChoice() {
	i := selectedPathIndex(dllText)
	dllPath = ""
	if i >= 0 && i < len(dllCandidates) {
		dllPath = dllCandidates[i].Path
	}
	sendMessage.Call(checkBox, 0x00f1, 0, 0)
	refreshMainActions()
	if dllPath != "" {
		text(statusText, "已找到 OPPO 候选文件："+dllPath+"\r\n请核对安装目录并确认手机连接；尚未修改文件。")
	}
}
func useProxyChoice() {
	i := selectedPathIndex(yamlText)
	yamlPath = ""
	selectedProxy = nil
	if i >= 0 && i < len(proxyCandidates) {
		selectedProxy = &proxyCandidates[i]
		yamlPath = selectedProxy.Path
		note := "未能定位配置。可重新识别，或手动指定。"
		if yamlPath != "" {
			switch selectedProxy.Client {
			case clientRev:
				note = "已识别 Clash Verge Rev；修复前请完全退出客户端。"
			case clientCFW:
				note = "已识别当前启用配置；订阅刷新可能覆盖本次修改。"
			default:
				note = "已手动指定配置；修复前请退出代理软件。"
			}
		}
		text(summaryText, note)
		if yamlPath != "" {
			text(statusText, "已识别 "+clientDisplayName(selectedProxy.Client)+"\r\n配置位置："+yamlPath)
		} else {
			text(statusText, selectedProxy.Note)
		}
	} else {
		text(summaryText, "请选择代理客户端，工具会自动定位配置。")
	}
	refreshMainActions()
}
func useManualDLL(path string) {
	stopPathSearch()
	dllCandidates = []pathCandidate{{Path: path, Label: path}}
	fillPathChoices(dllText, "请选择已找到的文件", dllCandidates)
	sendMessage.Call(dllText, comboSetSelection, 1, 0)
	useDLLChoice()
	searchedPaths[modeDLL] = true
}
func useManualProxy(path string) {
	stopPathSearch()
	proxyCandidates = []pathCandidate{{Path: path, Client: clientManual, Label: "手动指定 · " + path, Note: "已手动指定配置。修复前请退出代理软件，完成后重新打开。"}}
	fillPathChoices(yamlText, "请选择代理软件", proxyCandidates)
	sendMessage.Call(yamlText, comboSetSelection, 1, 0)
	useProxyChoice()
	searchedPaths[modeYAML] = true
}
func stopPathSearch() {
	if activePathSearch != nil {
		activePathSearch.cancel()
		searchedPaths[activePathSearch.mode] = false
		activePathSearch = nil
	}
	enable(dllFindButton, true)
	enable(proxyFindButton, true)
	enable(dllText, true)
	enable(yamlText, true)
}
func beginPathSearch(mode repairMode) {
	if mode != modeDLL && mode != modeYAML {
		return
	}
	stopPathSearch()
	pathSequence++
	token, owner := pathSequence, win
	ctx, cancel := context.WithCancel(context.Background())
	job := &pathSearchJob{token: token, mode: mode, cancel: cancel, result: make(chan pathSearchResult, 1)}
	activePathSearch = job
	control, find := dllText, dllFindButton
	if mode == modeDLL {
		dllPath = ""
		dllCandidates = nil
		sendMessage.Call(checkBox, 0x00f1, 0, 0)
		text(statusText, "正在从 OPPO 安装记录、运行程序和常见目录查找 DLL…")
	} else {
		control, find = yamlText, proxyFindButton
		yamlPath = ""
		selectedProxy = nil
		proxyCandidates = nil
		text(summaryText, "正在识别两个客户端的配置位置，请稍候…")
		text(statusText, "只读识别客户端配置，不修改任何文件。")
	}
	fillPathChoices(control, "正在自动识别…", nil)
	enable(control, false)
	enable(find, false)
	refreshMainActions()
	scan := pathDiscoverer
	go func() {
		result := scan(ctx, mode)
		job.result <- result
		if ctx.Err() == nil {
			postMessageProc.Call(owner, wmPathsDone, token, 0)
		}
	}()
}
func finishPathSearch(token uintptr) {
	if win == 0 || activePathSearch == nil || activePathSearch.token != token || currentMode != activePathSearch.mode {
		return
	}
	select {
	case result := <-activePathSearch.result:
		mode := activePathSearch.mode
		activePathSearch.cancel()
		activePathSearch = nil
		searchedPaths[mode] = true
		enable(dllText, true)
		enable(yamlText, true)
		enable(dllFindButton, true)
		enable(proxyFindButton, true)
		if mode == modeDLL {
			dllCandidates = result.Candidates
			fillPathChoices(dllText, "请选择已找到的 OPPO 文件", dllCandidates)
			if len(dllCandidates) == 1 {
				sendMessage.Call(dllText, comboSetSelection, 1, 0)
				useDLLChoice()
			} else if len(dllCandidates) > 1 {
				text(statusText, "找到多处 OPPO 候选文件，请在列表中选择实际安装目录；尚未修改。")
			} else {
				text(statusText, result.Note)
			}
		} else {
			proxyCandidates = result.Candidates
			fillPathChoices(yamlText, "请选择你正在使用的代理软件", proxyCandidates)
			valid, index := 0, -1
			for i, c := range proxyCandidates {
				if c.Path != "" {
					valid++
					index = i
				}
			}
			if valid == 1 {
				sendMessage.Call(yamlText, comboSetSelection, uintptr(index+1), 0)
				useProxyChoice()
			} else {
				text(summaryText, "请选择客户端，再扫描联网网卡；修复前请退出客户端。")
				text(statusText, "客户端识别完成；请选择列表中的代理软件。")
			}
		}
		refreshMainActions()
	default:
	}
}

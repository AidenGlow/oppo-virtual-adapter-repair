//go:build windows

package main

import (
	"context"
	"strings"
	"time"
)

const wmEffectivenessDone = 0x8004

type effectivenessJob struct {
	token  uintptr
	cancel context.CancelFunc
	result chan effectivenessResult
}

var (
	activeEffectivenessCheck *effectivenessJob
	effectivenessSequence    uintptr
	repairExpectations       = map[string]string{}
	effectivenessChecker     = checkWindowsEffectiveness
)

func rememberRepairExpectation(path, adapter string) {
	repairExpectations[strings.ToLower(path)] = adapter
}
func stopEffectivenessCheck() {
	if activeEffectivenessCheck != nil {
		activeEffectivenessCheck.cancel()
		activeEffectivenessCheck = nil
	}
}
func beginEffectivenessCheck() {
	if currentMode != modeYAML || selectedProxy == nil || yamlPath == "" || activeEffectivenessCheck != nil {
		return
	}
	candidate := *selectedProxy
	expected := repairExpectations[strings.ToLower(candidate.Path)]
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	effectivenessSequence++
	job := &effectivenessJob{effectivenessSequence, cancel, make(chan effectivenessResult, 1)}
	activeEffectivenessCheck = job
	owner, checker := win, effectivenessChecker
	text(statusText, "正在只读检查当前客户端的运行网卡设置…")
	refreshMainActions()
	go func() {
		job.result <- checker(ctx, candidate, expected)
		postMessageProc.Call(owner, wmEffectivenessDone, job.token, 0)
	}()
}
func finishEffectivenessCheck(token uintptr) {
	job := activeEffectivenessCheck
	if win == 0 || currentMode != modeYAML || job == nil || job.token != token {
		return
	}
	select {
	case result := <-job.result:
		job.cancel()
		activeEffectivenessCheck = nil
		text(statusText, result.Title+"\r\n"+result.Detail)
		refreshMainActions()
	default:
	}
}

//go:build windows

package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type fileUser struct {
	PID           uint32
	Name, Service string
}
type rmUniqueProcess struct {
	PID     uint32
	Started syscall.Filetime
}
type rmProcessInfo struct {
	Process               rmUniqueProcess
	Application           [256]uint16
	Service               [64]uint16
	Type, Status, Session uint32
	Restartable           int32
}

var restartManager = syscall.NewLazyDLL("rstrtmgr.dll")
var fileUserQuery = queryFileUsers
var fileUserSlots = make(chan struct{}, 1)

const fileUserTimeout = 1500 * time.Millisecond

func queryFileUsers(paths []string) ([]fileUser, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	var session uint32
	var key [33]uint16
	status, _, _ := restartManager.NewProc("RmStartSession").Call(uintptr(unsafe.Pointer(&session)), 0, uintptr(unsafe.Pointer(&key[0])))
	if status != 0 {
		return nil, syscall.Errno(status)
	}
	defer restartManager.NewProc("RmEndSession").Call(uintptr(session))
	names := make([]*uint16, len(paths))
	for i, path := range paths {
		names[i] = nativePath(path)
	}
	status, _, _ = restartManager.NewProc("RmRegisterResources").Call(uintptr(session), uintptr(len(names)), uintptr(unsafe.Pointer(&names[0])), 0, 0, 0, 0)
	runtime.KeepAlive(names)
	if status != 0 {
		return nil, syscall.Errno(status)
	}
	var records []rmProcessInfo
	for attempt := 0; attempt < 4; attempt++ {
		var needed, reboot uint32
		count := uint32(len(records))
		var buffer uintptr
		if len(records) > 0 {
			buffer = uintptr(unsafe.Pointer(&records[0]))
		}
		status, _, _ = restartManager.NewProc("RmGetList").Call(uintptr(session), uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&count)), buffer, uintptr(unsafe.Pointer(&reboot)))
		runtime.KeepAlive(records)
		if status == 234 {
			if needed == 0 || needed > 1024 {
				return nil, errors.New("too many file users")
			}
			records = make([]rmProcessInfo, needed)
			continue
		}
		if status != 0 {
			return nil, syscall.Errno(status)
		}
		if int(count) > len(records) {
			return nil, errors.New("invalid process count")
		}
		users := []fileUser{}
		seen := map[uint32]bool{}
		for _, record := range records[:count] {
			if record.Process.PID == 0 || seen[record.Process.PID] {
				continue
			}
			seen[record.Process.PID] = true
			users = append(users, fileUser{record.Process.PID, syscall.UTF16ToString(record.Application[:]), syscall.UTF16ToString(record.Service[:])})
		}
		sort.Slice(users, func(i, j int) bool { return users[i].PID < users[j].PID })
		return users, nil
	}
	return nil, errors.New("file user list changed repeatedly")
}
func occupancyPaths(err error, fallback string) []string {
	candidates := []string{fallback}
	var pathError *os.PathError
	var linkError *os.LinkError
	if errors.As(err, &pathError) {
		candidates = append(candidates, pathError.Path)
	}
	if errors.As(err, &linkError) {
		candidates = append(candidates, linkError.Old, linkError.New)
	}
	paths := []string{}
	seen := map[string]bool{}
	for _, candidate := range candidates {
		if candidate == "" {
			continue
		}
		absolute, e := filepath.Abs(candidate)
		if e != nil || ordinary(absolute) != nil {
			continue
		}
		key := strings.ToLower(absolute)
		if !seen[key] {
			seen[key] = true
			paths = append(paths, absolute)
		}
	}
	return paths
}
func occupancyFailureHint(err error, mode repairMode, fallback string) string {
	var code syscall.Errno
	if !errors.As(err, &code) || !(code == 32 || code == 33 || mode == modeDLL && code == 5) {
		return ""
	}
	paths := occupancyPaths(err, fallback)
	if len(paths) == 0 {
		return ""
	}
	type answer struct {
		users []fileUser
		err   error
	}
	var result answer
	select {
	case fileUserSlots <- struct{}{}:
	default:
		return "\n\n占用程序：已有查询尚未完成，请稍后重试。"
	}
	query := fileUserQuery
	done := make(chan answer, 1)
	go func() { defer func() { <-fileUserSlots }(); users, e := query(paths); done <- answer{users, e} }()
	timer := time.NewTimer(fileUserTimeout)
	defer timer.Stop()
	select {
	case result = <-done:
	case <-timer.C:
		return "\n\n占用程序：本次查询超时，请按上方操作指引处理后重试。"
	}
	if result.err != nil {
		return "\n\n占用程序：当前无法读取进程信息，请按上方指引处理；这不表示文件未被占用。"
	}
	if len(result.users) == 0 {
		return "\n\n占用程序：未识别到具体程序，可能已退出或无法读取；请按上方指引继续排查。"
	}
	detail := "\n\n检测到使用相关文件的程序（查询时）："
	for i, user := range result.users {
		if i == 8 {
			detail += fmt.Sprintf("\n另有 %d 个程序。", len(result.users)-i)
			break
		}
		name := strings.TrimSpace(strings.NewReplacer("\r", " ", "\n", " ", "\t", " ").Replace(user.Name))
		if name == "" {
			name = "未命名程序"
		}
		detail += fmt.Sprintf("\n%s（PID %d）", name, user.PID)
		if user.Service != "" {
			detail += " · 服务：" + strings.NewReplacer("\r", " ", "\n", " ").Replace(user.Service)
		}
	}
	return detail + "\n请保存相关程序中的工作，正常退出后重试；后台服务请联系管理员处理。"
}

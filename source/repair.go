package main

import (
	"bytes"
	"debug/pe"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

const suffix = "wintun.dll.oppo互联"

type Adapter struct {
	Name          string `json:"Name"`
	Description   string `json:"Description"`
	Status        string `json:"Status"`
	Hardware      bool   `json:"Hardware"`
	HardwareKnown bool   `json:"-"`
	Index         int    `json:"IfIndex"`
	MAC           string `json:"MacAddress"`
	Gateway       string `json:"-"`
	Recommended   bool   `json:"-"`
	HasRoute      bool   `json:"-"`
	RouteKnown    bool   `json:"-"`
}
type route struct {
	Index   int    `json:"IfIndex"`
	Gateway string `json:"Gateway"`
}
type snapshot struct {
	Adapters []struct {
		Name        string `json:"Name"`
		Description string `json:"Description"`
		Status      string `json:"Status"`
		Hardware    *bool  `json:"Hardware"`
		Index       int    `json:"IfIndex"`
		MAC         string `json:"MacAddress"`
	} `json:"Adapters"`
	Routes      []route `json:"Routes"`
	RoutesKnown *bool   `json:"RoutesKnown"`
}

func parseAdapters(b []byte) ([]Adapter, error) {
	var s snapshot
	b = bytes.TrimPrefix(bytes.TrimSpace(b), []byte{0xef, 0xbb, 0xbf})
	if e := json.Unmarshal(b, &s); e != nil {
		return nil, fmt.Errorf("解析网卡信息失败：%w", e)
	}
	if len(s.Adapters) == 0 {
		return nil, errors.New("Windows 没有返回任何网卡")
	}
	routeMap := map[int]bool{}
	gatewayMap := map[int]string{}
	for _, r := range s.Routes {
		routeMap[r.Index] = true
		if ip := net.ParseIP(r.Gateway); ip != nil && !ip.IsUnspecified() && !ip.IsLoopback() {
			gatewayMap[r.Index] = r.Gateway
		}
	}
	knownRoutes := s.Routes != nil
	if s.RoutesKnown != nil {
		knownRoutes = *s.RoutesKnown
	}
	adapters := make([]Adapter, 0, len(s.Adapters))
	for _, raw := range s.Adapters {
		if strings.TrimSpace(raw.Name) == "" {
			return nil, errors.New("扫描到无名称网卡")
		}
		a := Adapter{Name: raw.Name, Description: strings.TrimSpace(raw.Description), Status: raw.Status, Index: raw.Index, MAC: raw.MAC, HardwareKnown: raw.Hardware != nil, RouteKnown: knownRoutes}
		if raw.Hardware != nil {
			a.Hardware = *raw.Hardware
		}
		a.HasRoute = knownRoutes && routeMap[a.Index]
		a.Gateway = gatewayMap[a.Index]
		adapters = append(adapters, a)
	}
	sort.SliceStable(adapters, func(i, j int) bool {
		a, b := adapters[i], adapters[j]
		if a.Hardware != b.Hardware {
			return a.Hardware
		}
		up := strings.EqualFold(a.Status, "Up")
		upb := strings.EqualFold(b.Status, "Up")
		if up != upb {
			return up
		}
		return strings.ToLower(a.Name) < strings.ToLower(b.Name)
	})
	return adapters, nil
}
func adapterKind(a Adapter) string {
	if !a.HardwareKnown {
		return "硬件属性未能读取，请自行核对"
	}
	if a.Hardware {
		return "物理/硬件网卡（Windows报告）"
	}
	return "虚拟/非硬件网卡（Windows报告）"
}
func adapterState(a Adapter) string {
	switch strings.ToLower(a.Status) {
	case "up":
		return "已连接"
	case "down", "disconnected":
		return "未连接"
	case "disabled":
		return "已禁用"
	default:
		if a.Status == "" {
			return "未知状态"
		}
		return a.Status
	}
}

func dllPair(selected string) (orig, changed string, e error) {
	if selected == "" {
		return "", "", errors.New("请先选择目标文件")
	}
	abs, e := filepath.Abs(filepath.Clean(selected))
	if e != nil {
		return "", "", e
	}
	base := filepath.Base(abs)
	if !strings.EqualFold(base, "wintun.dll") && base != suffix {
		return "", "", errors.New("只能选择 wintun.dll 或 wintun.dll.oppo互联")
	}
	if !strings.EqualFold(filepath.Base(filepath.Dir(abs)), "daemon") {
		return "", "", errors.New("文件必须位于 OPPO 互联 daemon 目录内")
	}
	d := filepath.Dir(abs)
	return filepath.Join(d, "wintun.dll"), filepath.Join(d, suffix), nil
}
func ordinary(p string) error {
	st, e := os.Lstat(p)
	if e != nil {
		return e
	}
	if !st.Mode().IsRegular() {
		return errors.New("目标不是普通文件")
	}
	return nil
}
func isWindowsBinary(p string) error {
	if e := ordinary(p); e != nil {
		return e
	}
	f, e := os.Open(p)
	if e != nil {
		return e
	}
	defer f.Close()
	image, e := pe.NewFile(f)
	if e != nil {
		return fmt.Errorf("文件不是有效的 Windows PE：%w", e)
	}
	defer image.Close()
	if image.FileHeader.Characteristics&0x2000 == 0 || image.OptionalHeader == nil || len(image.Sections) == 0 {
		return errors.New("所选 PE 文件不是有效 DLL")
	}
	info, e := f.Stat()
	if e != nil {
		return e
	}
	for _, section := range image.Sections {
		if uint64(section.Offset)+uint64(section.Size) > uint64(info.Size()) {
			return errors.New("DLL 节区超出文件范围，文件可能已损坏")
		}
	}
	return nil
}
func changeDLL(selected string, restore bool) (string, error) {
	a, b, e := dllPair(selected)
	if e != nil {
		return "", e
	}
	if restore {
		a, b = b, a
	}
	if e = checkDLLPath(a); e != nil {
		return "", e
	}
	if e = isWindowsBinary(a); e != nil {
		return "", e
	}
	if _, e = os.Lstat(b); e == nil {
		return "", errors.New("目标文件已存在，为防止覆盖已取消")
	}
	if !os.IsNotExist(e) {
		return "", e
	}
	if e = renameNoReplace(a, b); e != nil {
		return "", fmt.Errorf("重命名 DLL 失败：%w", e)
	}
	return b, nil
}
func backupFile(p string) (string, error) {
	if e := ordinary(p); e != nil {
		return "", e
	}
	source, e := os.Open(p)
	if e != nil {
		return "", e
	}
	defer source.Close()
	_, e = source.Stat()
	if e != nil {
		return "", e
	}
	for n := 0; n < 100; n++ {
		tag := time.Now().Format("20060102-150405.000")
		if n > 0 {
			tag += fmt.Sprintf("-%d", n)
		}
		backup := p + ".oppo-backup-" + tag + ".bak"
		err := copyNewFile(p, backup)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		out, err := os.OpenFile(backup, os.O_WRONLY, 0)
		if err != nil {
			os.Remove(backup)
			return "", err
		}
		err = out.Sync()
		closeErr := out.Close()
		if err != nil || closeErr != nil {
			os.Remove(backup)
			if err != nil {
				return "", err
			}
			return "", closeErr
		}
		return backup, nil
	}
	return "", errors.New("无法建立唯一备份")
}
func replace(p string, b []byte) error {
	if e := ordinary(p); e != nil {
		return e
	}
	st, e := os.Stat(p)
	if e != nil {
		return e
	}
	f, e := os.CreateTemp(filepath.Dir(p), ".oppo-repair-*.tmp")
	if e != nil {
		return e
	}
	n := f.Name()
	defer os.Remove(n)
	if e = f.Chmod(st.Mode().Perm()); e != nil {
		f.Close()
		return e
	}
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return replaceNative(p, n)
}

func updateYaml(path, adapter string) (string, error) {
	if e := validateYAMLPath(path); e != nil {
		return "", e
	}
	if e := ordinary(path); e != nil {
		return "", e
	}
	guard, e := openYAMLGuard(path)
	if e != nil {
		return "", e
	}
	defer guard.Close()
	info, e := guard.source.Stat()
	if e != nil {
		return "", e
	}
	original, e := io.ReadAll(guard.source)
	if e != nil {
		return "", e
	}
	edited, e := yamlEdit(original, adapter)
	if e != nil {
		return "", e
	}
	if bytes.Equal(edited, original) {
		return "", errors.New("文件已经是相同配置，不需要修改")
	}
	backup, e := backupFile(path)
	if e != nil {
		return "", e
	}
	backed, e := os.ReadFile(backup)
	if e != nil || !bytes.Equal(backed, original) {
		return backup, errors.New("备份与编辑快照不一致，已停止；未写入配置")
	}
	if e = checkFileSnapshot(path, info, original); e != nil {
		return backup, e
	}
	if e = writeNetworkReceipt(backup, adapter, original); e != nil {
		return backup, fmt.Errorf("保存网卡撤销记录失败，未写入配置：%w", e)
	}
	guard.closeSource()
	if e = checkFileSnapshot(path, info, original); e != nil {
		return backup, e
	}
	if e = replace(path, edited); e != nil {
		return backup, fmt.Errorf("原始文件已经备份到 %s，但写入失败：%w", backup, e)
	}
	return backup, nil
}

var suffixRE = regexp.MustCompile(`(?i)\.oppo-backup-\d{8}-\d{6}\.\d{3}(?:-\d+)?\.bak$`)

func restoreYaml(backup string) (string, string, error) {
	plan, e := prepareYAMLRestore(backup)
	if e != nil {
		return "", "", e
	}
	return applyYAMLRestore(plan)
}

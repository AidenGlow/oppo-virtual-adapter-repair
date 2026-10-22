package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

type effectivenessResult struct{ State, Title, Detail string }
type coreProcess struct {
	PID                          int
	DataDir, ConfigPath, Started string
	Ports                        []int
	ControllerOverride           bool
}
type runtimeSettings struct {
	Interface *string `json:"interface-name"`
	Tun       *struct {
		Enable     *bool `json:"enable"`
		AutoDetect *bool `json:"auto-detect-interface"`
	} `json:"tun"`
}
type runtimeEndpoint struct{ Address, Pipe, Secret string }
type runtimeDependencies struct {
	Inspect func(context.Context) ([]coreProcess, error)
	Fetch   func(context.Context, coreProcess, runtimeEndpoint) ([]byte, error)
}

func sameConfigPath(a, b string) bool {
	return a != "" && b != "" && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}
func coreMatchesCandidate(core coreProcess, candidate pathCandidate) bool {
	if candidate.Client == clientManual {
		return sameConfigPath(core.ConfigPath, candidate.Path)
	}
	root := filepath.Dir(candidate.Path)
	if candidate.Client == clientCFW {
		root = filepath.Dir(root)
	}
	return sameConfigPath(core.DataDir, root) || sameConfigPath(filepath.Dir(core.ConfigPath), root)
}
func fixedAdapter(raw []byte) (string, error) {
	doc, err := parseYAMLDocument(raw)
	if err != nil {
		return "", err
	}
	root := doc.Content[0]
	_, name := yamlField(root, "interface-name")
	_, tun := yamlField(root, "tun")
	if name == nil || name.Tag != "!!str" || strings.TrimSpace(name.Value) == "" || tun == nil {
		return "", fmt.Errorf("missing fixed interface")
	}
	_, auto := yamlField(tun, "auto-detect-interface")
	if auto == nil || auto.Tag != "!!bool" || auto.Value != "false" {
		return "", fmt.Errorf("auto detect not disabled")
	}
	return name.Value, nil
}
func endpointFromYAML(raw []byte) (runtimeEndpoint, error) {
	doc, err := parseYAMLDocument(raw)
	if err != nil {
		return runtimeEndpoint{}, err
	}
	var e runtimeEndpoint
	for key, dst := range map[string]*string{"external-controller": &e.Address, "external-controller-pipe": &e.Pipe, "secret": &e.Secret} {
		_, v := yamlField(doc.Content[0], key)
		if v != nil && v.Tag != "!!null" {
			if v.Tag != "!!str" {
				return e, fmt.Errorf("unsupported controller value")
			}
			*dst = v.Value
		}
	}
	if strings.ContainsAny(e.Secret, "\r\n\x00") {
		return runtimeEndpoint{}, fmt.Errorf("invalid controller secret")
	}
	if e.Pipe != "" {
		const prefix = `\\.\pipe\`
		if !strings.HasPrefix(strings.ToLower(e.Pipe), prefix) || len(e.Pipe) == len(prefix) || strings.ContainsAny(e.Pipe[len(prefix):], `/\:`+"\x00\r\n") {
			return runtimeEndpoint{}, fmt.Errorf("not a local named pipe")
		}
	}
	if e.Address != "" {
		host, port, err := net.SplitHostPort(e.Address)
		if err != nil {
			return e, err
		}
		number, err := strconv.Atoi(port)
		if err != nil || number < 1 || number > 65535 {
			return e, fmt.Errorf("invalid controller port")
		}
		ip := net.ParseIP(host)
		if host == "localhost" || host == "*" || host == "" || (ip != nil && ip.IsUnspecified()) {
			host = "127.0.0.1"
		} else if ip == nil || !ip.IsLoopback() {
			return runtimeEndpoint{}, fmt.Errorf("controller is not loopback")
		}
		e.Address = net.JoinHostPort(host, port)
	}
	if e.Address == "" && e.Pipe == "" {
		return e, fmt.Errorf("no local controller")
	}
	return e, nil
}
func compareRuntimeSettings(raw []byte, expected string) effectivenessResult {
	var s runtimeSettings
	if json.Unmarshal(raw, &s) != nil || s.Interface == nil || s.Tun == nil || s.Tun.AutoDetect == nil || s.Tun.Enable == nil {
		return effectivenessResult{"unknown", "无法确认是否生效", "当前内核没有返回完整的网卡 / TUN 字段。请在客户端检查运行配置；工具没有改动文件。"}
	}
	if *s.Interface != expected || *s.Tun.AutoDetect {
		return effectivenessResult{"mismatch", "运行设置与修复设置不一致", fmt.Sprintf("本次应使用：%s；内核当前使用：%s。\r\n请重载配置或重新打开所选客户端；若仍不一致，请检查扩展配置 / 脚本是否覆盖设置。", expected, *s.Interface)}
	}
	if !*s.Tun.Enable {
		return effectivenessResult{"tun-off", "网卡设置已采用，TUN 尚未开启", "请在代理客户端开启 TUN，再点击‘检查是否生效’。"}
	}
	return effectivenessResult{"effective", "配置已生效（当前运行内核）", "内核已采用网卡：" + expected + "，自动选择出口已关闭，TUN 已开启。\r\n可再打开常用网页确认实际联网情况。"}
}
func checkEffectiveness(ctx context.Context, candidate pathCandidate, expected string, deps runtimeDependencies) effectivenessResult {
	unknown := func(detail string) effectivenessResult {
		return effectivenessResult{"unknown", "无法确认是否生效", detail}
	}
	if candidate.Path == "" {
		return unknown("请先选择代理客户端或手动指定配置。")
	}
	raw, sourceInfo, _, err := readDiscoveryYAML(candidate.Path)
	if err != nil {
		return unknown("配置路径不可读取，请重新识别或手动指定。")
	}
	adapter, err := fixedAdapter(raw)
	if err != nil {
		return effectivenessResult{"not-fixed", "当前文件未设置固定联网网卡", "请先选择联网网卡并修复，再重新打开客户端进行检查。"}
	}
	if expected != "" && adapter != expected {
		return effectivenessResult{"source-changed", "修复后的文件设置已变化", "本次选择的是：" + expected + "；当前文件中是：" + adapter + "。请核对订阅或客户端设置是否覆盖了修改。"}
	}
	if candidate.Client == clientCFW {
		current, err := cfwCurrentTarget(filepath.Dir(filepath.Dir(candidate.Path)))
		if err != nil || !sameConfigPath(current.Path, candidate.Path) {
			return unknown("CFW 当前启用配置已变化或无法读取，请重新识别后检查。")
		}
	}
	cores, err := deps.Inspect(ctx)
	if err != nil {
		return unknown("无法读取运行内核信息，请重试或在客户端查看运行配置。")
	}
	if len(cores) == 0 {
		return effectivenessResult{"not-running", "尚未加载：未发现运行中的代理内核", "请重新打开所选代理客户端，启用 TUN 后再次检查。"}
	}
	matches := []coreProcess{}
	for _, core := range cores {
		if coreMatchesCandidate(core, candidate) {
			matches = append(matches, core)
		}
	}
	if len(matches) != 1 {
		return unknown("未能唯一对应所选配置的运行内核。请重新识别当前客户端后检查；不会猜测其他客户端的结果。")
	}
	core := matches[0]
	if core.ControllerOverride {
		return unknown("内核通过启动参数另行设置了控制接口，当前无法可靠检查。请在客户端查看运行配置。")
	}
	endpointPath := core.ConfigPath
	if endpointPath == "" {
		return unknown("无法确定内核运行配置的位置，请在客户端查看运行配置。")
	}
	endpointRaw, endpointInfo, _, err := readDiscoveryYAML(endpointPath)
	if err != nil {
		return unknown("运行配置不可读取，请重新打开客户端后重试。")
	}
	endpoint, err := endpointFromYAML(endpointRaw)
	if err != nil {
		return unknown("没有可读取的本机控制接口。请在客户端查看运行配置；工具不会开启或改写接口。")
	}
	response, err := deps.Fetch(ctx, core, endpoint)
	if err != nil {
		return unknown("无法读取所选内核的本机控制接口，可能未开放、认证失败或接口不兼容。请确认客户端已启动后重试。")
	}
	again, info, _, err := readDiscoveryYAML(candidate.Path)
	if err != nil || !os.SameFile(sourceInfo, info) || !bytes.Equal(raw, again) {
		return unknown("检查期间配置文件已变化，请重新识别并检查。")
	}
	again, info, _, err = readDiscoveryYAML(endpointPath)
	if err != nil || !os.SameFile(endpointInfo, info) || !bytes.Equal(endpointRaw, again) {
		return unknown("检查期间运行配置已变化，请稍后重试。")
	}
	if candidate.Client == clientCFW {
		current, err := cfwCurrentTarget(filepath.Dir(filepath.Dir(candidate.Path)))
		if err != nil || !sameConfigPath(current.Path, candidate.Path) {
			return unknown("检查期间 CFW 切换了配置，请重新识别。")
		}
	}
	if ctx.Err() != nil {
		return unknown("检查已取消或超时，可以重试。")
	}
	return compareRuntimeSettings(response, adapter)
}

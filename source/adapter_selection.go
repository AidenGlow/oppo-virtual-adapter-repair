package main

import (
	"net"
	"sort"
	"strings"
)

func connectedPhysical(a Adapter) bool {
	return a.HardwareKnown && a.Hardware && strings.EqualFold(a.Status, "Up")
}

// 只有唯一一张已连接的物理网卡具备明确的默认网关时才推荐，绝不代选。
func markRecommendedAdapters(all []Adapter) []Adapter {
	out := append([]Adapter(nil), all...)
	candidate := -1
	for i := range out {
		out[i].Recommended = false
	}
	for i := range out {
		ip := net.ParseIP(out[i].Gateway)
		if connectedPhysical(out[i]) && out[i].RouteKnown && out[i].HasRoute && ip != nil && ip.To4() != nil && !ip.IsUnspecified() && !ip.IsLoopback() {
			if candidate >= 0 {
				candidate = -2
				break
			}
			candidate = i
		}
	}
	if candidate >= 0 {
		out[candidate].Recommended = true
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Recommended && !out[j].Recommended })
	return out
}

func visibleAdapters(all []Adapter, showAll bool) []Adapter {
	out := make([]Adapter, 0, len(all))
	for _, a := range all {
		if showAll || connectedPhysical(a) {
			out = append(out, a)
		}
	}
	return out
}

func adapterRecommendation(a Adapter) string {
	if a.Recommended {
		return "建议选择：已连接的物理网卡，且是唯一具有明确 IPv4 默认网关的候选（仍需本人核对）"
	}
	return "未自动推荐；请核对正在使用的 Wi-Fi / 网线，多网卡环境需自行判断"
}

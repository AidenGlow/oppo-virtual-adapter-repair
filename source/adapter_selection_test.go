package main

import (
	"strings"
	"testing"
)

func TestAdapterSelectionFiltersAndRecommendsOnlyUniqueGateway(t *testing.T) {
	all := []Adapter{
		{Name: "Ethernet", Hardware: true, HardwareKnown: true, Status: "Up", RouteKnown: true},
		{Name: "WLAN", Hardware: true, HardwareKnown: true, Status: "Up", RouteKnown: true, HasRoute: true, Gateway: "192.168.1.1"},
		{Name: "TUN", HardwareKnown: true, Status: "Up", RouteKnown: true, HasRoute: true, Gateway: "198.18.0.1"},
		{Name: "Disconnected", HardwareKnown: true, Hardware: true, Status: "Down"},
		{Name: "Unknown", Status: "Up"},
	}
	marked := markRecommendedAdapters(all)
	filtered := visibleAdapters(marked, false)
	if len(filtered) != 2 || filtered[0].Name != "WLAN" || !filtered[0].Recommended || filtered[1].Recommended {
		t.Fatal(filtered)
	}
	if all[1].Recommended {
		t.Fatal("modified scan input")
	}
	if len(visibleAdapters(marked, true)) != len(all) {
		t.Fatal("show all lost adapters")
	}
	if !strings.Contains(adapterRecommendation(filtered[0]), "默认网关") {
		t.Fatal("missing reason")
	}
	all[0].HasRoute = true
	all[0].Gateway = "10.0.0.1"
	for _, a := range markRecommendedAdapters(all) {
		if a.Recommended {
			t.Fatal("multiple exits were guessed", a)
		}
	}
}
func TestAdapterRecommendationDoesNotGuessUnknownOrInvalidGateway(t *testing.T) {
	for _, gateway := range []string{"", "0.0.0.0", "127.0.0.1", "not-an-ip", "::1", "fe80::1"} {
		a := Adapter{Name: " WLAN ", HardwareKnown: true, Hardware: true, Status: "Up", RouteKnown: true, HasRoute: true, Gateway: gateway}
		if markRecommendedAdapters([]Adapter{a})[0].Recommended {
			t.Fatal("invalid gateway recommended", gateway)
		}
	}
	a := Adapter{Name: " WLAN ", HardwareKnown: true, Hardware: true, Status: "Up", HasRoute: true, Gateway: "192.168.1.1", Recommended: true}
	result := markRecommendedAdapters([]Adapter{a})
	if result[0].Recommended || result[0].Name != " WLAN " {
		t.Fatal("unknown route or exact name mishandled", result)
	}
	if len(visibleAdapters(nil, false)) != 0 || len(visibleAdapters([]Adapter{{Status: "Up"}}, false)) != 0 {
		t.Fatal("unknown hardware leaked into simple view")
	}
}

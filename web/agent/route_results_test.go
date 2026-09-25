package agent

import (
	"net"
	"strings"
	"testing"
)

func TestClassifyRoute(t *testing.T) {
	hop := func(address string, asns ...string) routeHop {
		country := ""
		if strings.HasPrefix(address, "59.43.") || strings.HasPrefix(address, "202.97.") {
			country = "CN"
		}
		return routeHop{address: net.ParseIP(address), asns: asns, country: country}
	}
	tests := []struct {
		name string
		hops []routeHop
		want string
	}{
		{"CN2 GIA after overseas gateway", []routeHop{hop("1.1.1.1", "23764"), hop("59.43.1.1", "4809")}, "CTGGIA"},
		{"CN2 GIA without CTG gateway", []routeHop{hop("59.43.1.1", "4809")}, "CN2GIA"},
		{"CN2 GT switches to 163", []routeHop{hop("59.43.1.1", "4809"), hop("202.97.1.1", "4134")}, "CN2GT"},
		{"CN2 GIA may later cross 163 near destination", []routeHop{hop("1.1.1.1", "64500"), hop("59.43.1.1", "4809"), hop("202.97.1.1", "4134")}, "CN2GIA"},
		{"GeoIP mislocates an overseas 163 hand-off before CN2 GIA", []routeHop{
			{address: net.ParseIP("19.41.250.250"), country: "US", ttl: 1},
			{address: net.ParseIP("19.41.250.158"), country: "US", ttl: 2},
			{address: net.ParseIP("218.30.48.141"), country: "CN", asns: []string{"4134"}, ttl: 3},
			{address: net.ParseIP("59.43.189.41"), country: "CN", ttl: 4},
			{address: net.ParseIP("59.43.16.165"), country: "CN", ttl: 6},
			{address: net.ParseIP("211.136.204.78"), country: "CN", asns: []string{"56040"}, ttl: 12},
		}, "CN2GIA"},
		{"one later CN2 hop does not override 163", []routeHop{
			{address: net.ParseIP("19.41.250.250"), country: "US", ttl: 1},
			{address: net.ParseIP("218.30.48.141"), country: "CN", asns: []string{"4134"}, ttl: 3},
			{address: net.ParseIP("59.43.189.41"), country: "CN", ttl: 4},
		}, "163"},
		{"confirmed 202.97 entry remains 163", []routeHop{
			{address: net.ParseIP("19.41.250.250"), country: "US", ttl: 1},
			{address: net.ParseIP("202.97.1.1"), country: "CN", asns: []string{"4134"}, ttl: 3},
			{address: net.ParseIP("59.43.189.41"), country: "CN", ttl: 4},
			{address: net.ParseIP("59.43.16.165"), country: "CN", ttl: 6},
		}, "163"},
		{"no observed foreign hop keeps 163 entry", []routeHop{
			{address: net.ParseIP("218.30.48.141"), country: "CN", asns: []string{"4134"}, ttl: 3},
			{address: net.ParseIP("59.43.189.41"), country: "CN", ttl: 4},
			{address: net.ParseIP("59.43.16.165"), country: "CN", ttl: 6},
		}, "163"},
		{"distant CN2 segment does not override 163 entry", []routeHop{
			{address: net.ParseIP("19.41.250.250"), country: "US", ttl: 1},
			{address: net.ParseIP("218.30.48.141"), country: "CN", asns: []string{"4134"}, ttl: 3},
			{address: net.ParseIP("59.43.189.41"), country: "CN", ttl: 9},
			{address: net.ParseIP("59.43.16.165"), country: "CN", ttl: 10},
		}, "163"},
		{"59.43 overrides a conflicting ASN like NetQuality", []routeHop{hop("1.1.1.1", "4134"), hop("59.43.189.41", "4134")}, "CN2GIA"},
		{"late CTGNet still identifies CTG GIA", []routeHop{hop("1.1.1.1", "64500"), hop("59.43.1.1", "4809"), hop("2.2.2.2", "23764"), hop("202.97.1.1", "4134")}, "CTGGIA"},
		{"CN2 GT skips CTG gateway at first China hop", []routeHop{hop("59.43.1.1", "4809"), hop("2.2.2.2", "23764"), hop("202.97.1.1", "4134")}, "CN2GT"},
		{"CN2 without confirmed China entry", []routeHop{hop("1.1.1.1", "4809"), hop("2.2.2.2", "4134")}, "CN2"},
		{"CN2 after unrecognized first China hop", []routeHop{{address: net.ParseIP("1.1.1.1"), country: "CN", asns: []string{"64500"}}, hop("59.43.1.1", "4809")}, "CN2"},
		{"first backbone matters", []routeHop{hop("202.97.1.1", "4134"), hop("59.43.1.1", "4809")}, "163"},
		{"CN2 prefix beats earlier generic ASN", []routeHop{hop("1.1.1.1", "4134"), hop("59.43.1.1", "4809")}, "CN2GIA"},
		{"163 by ASN", []routeHop{hop("1.1.1.1", "4134")}, "163"},
		{"CTGNet alone is not GIA", []routeHop{hop("1.1.1.1", "23764")}, ""},
		{"Unicom 9929", []routeHop{hop("1.1.1.1", "9929"), hop("2.2.2.2", "4837")}, "9929"},
		{"unrecognized first China hop falls back to later AS9929", []routeHop{
			{address: net.ParseIP("210.14.165.41"), country: "CN", ttl: 5},
			{address: net.ParseIP("218.105.131.197"), country: "CN", asns: []string{"9929"}, ttl: 6},
			{address: net.ParseIP("219.158.121.85"), country: "CN", asns: []string{"4837"}, ttl: 9},
		}, "9929"},
		{"fallback prefers 9929 over later 163", []routeHop{{address: net.ParseIP("210.14.165.41"), country: "CN"}, hop("218.105.131.197", "9929"), hop("202.97.1.1", "4134")}, "9929"},
		{"fallback prefers CMIN2 over 9929", []routeHop{{address: net.ParseIP("210.14.165.41"), country: "CN"}, hop("218.105.131.197", "9929"), hop("2.2.2.2", "58807")}, "CMIN2"},
		{"fallback prefers 10099 over CN2", []routeHop{{address: net.ParseIP("210.14.165.41"), country: "CN"}, hop("2.2.2.2", "10099"), hop("59.43.1.1", "4809")}, "10099"},
		{"fallback prefers CN2 over CMI", []routeHop{{address: net.ParseIP("210.14.165.41"), country: "CN"}, hop("59.43.1.1", "4809"), hop("2.2.2.2", "9808")}, "CN2"},
		{"fallback prefers CMI over 163", []routeHop{{address: net.ParseIP("210.14.165.41"), country: "CN"}, hop("2.2.2.2", "9808"), hop("3.3.3.3", "4134")}, "CMI"},
		{"recognized first China 163 is not overridden by later 9929", []routeHop{hop("202.97.1.1", "4134"), hop("218.105.131.197", "9929")}, "163"},
		{"Unicom 10099 gateway", []routeHop{hop("1.1.1.1", "10099"), hop("2.2.2.2", "4837")}, "10099"},
		{"10099 entry upgrades to 9929 when visible", []routeHop{{address: net.ParseIP("210.14.165.41"), country: "CN", asns: []string{"10099"}}, hop("218.105.131.197", "9929")}, "9929"},
		{"Unicom 4837", []routeHop{hop("1.1.1.1", "4837")}, "4837"},
		{"Unicom 4808 is not identified by NetQuality", []routeHop{hop("1.1.1.1", "4808")}, ""},
		{"Mobile CMIN2", []routeHop{hop("1.1.1.1", "58807"), hop("2.2.2.2", "9808")}, "CMIN2"},
		{"Mobile CMI", []routeHop{hop("1.1.1.1", "58453"), hop("2.2.2.2", "9808")}, "CMI"},
		{"Mobile AS9808 uses NetQuality CMI label", []routeHop{hop("1.1.1.1", "9808")}, "CMI"},
		{"CERNET mainland entry", []routeHop{{address: net.ParseIP("101.6.6.1"), country: "CN", asns: []string{"4538"}}}, "CERNET"},
		{"CSTNET mainland entry", []routeHop{{address: net.ParseIP("159.226.1.1"), country: "CN", asns: []string{"7497"}}}, "CSTNET"},
		{"IPv6 backbone", []routeHop{hop("2001:db8::1", "9929")}, "9929"},
		{"unknown", []routeHop{hop("1.1.1.1", "64500")}, ""},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := classifyRoute(test.hops); got != test.want {
				t.Fatalf("classifyRoute(%v) = %q, want %q", test.hops, got, test.want)
			}
		})
	}
}

func TestParseRouteHopsKeepsOriginalTTL(t *testing.T) {
	hops := parseRouteHops([]string{"", "59.43.1.1", "", "202.97.1.1", "59.43.1.1"})
	if len(hops) != 2 || hops[0].ttl != 2 || hops[1].ttl != 4 {
		t.Fatalf("original TTL positions were lost: %+v", hops)
	}
}

func TestCymruDNSName(t *testing.T) {
	if got := cymruDNSName(net.ParseIP("216.90.108.31")); got != "31.108.90.216.origin.asn.cymru.com" {
		t.Fatalf("IPv4 DNS name = %q", got)
	}
	got := cymruDNSName(net.ParseIP("2001:db8::1"))
	if !strings.HasPrefix(got, "1.0.0.0.0.0.0.0.") || !strings.HasSuffix(got, ".2.origin6.asn.cymru.com") {
		t.Fatalf("IPv6 DNS name = %q", got)
	}
}

package agent

import (
	"net"
	"strconv"
	"strings"
)

// routeHop preserves the original TTL, including gaps caused by hidden hops.
type routeHop struct {
	address net.IP
	asns    []string
	country string
	ttl     int
}

func parseRouteHops(hops []string) []routeHop {
	seen := make(map[string]bool)
	var ordered []routeHop
	for index, hop := range hops {
		ip := net.ParseIP(hop)
		if ip == nil || !ip.IsGlobalUnicast() || ip.IsPrivate() {
			continue
		}
		if !seen[ip.String()] {
			seen[ip.String()] = true
			ordered = append(ordered, routeHop{address: ip, ttl: index + 1})
		}
	}
	if len(ordered) > 20 {
		ordered = ordered[len(ordered)-20:]
	}
	return ordered
}

func hasASN(hop routeHop, asn string) bool {
	for _, candidate := range hop.asns {
		if candidate == asn {
			return true
		}
	}
	return false
}

func inIPv4Prefix(hop routeHop, first, second byte) bool {
	ip := hop.address.To4()
	return ip != nil && ip[0] == first && ip[1] == second
}

func hasASNIn(hops []routeHop, asn string) bool {
	for _, hop := range hops {
		if hasASN(hop, asn) {
			return true
		}
	}
	return false
}

func classifyRoute(hops []routeHop) string {
	// NetQuality uses the first mainland hop as the entry, then falls back to
	// visible ASNs across the whole path when that hop is unrecognized.
	firstChina := -1
	for index, hop := range hops {
		if hop.country == "CN" {
			firstChina = index
			break
		}
	}
	if firstChina >= 0 {
		// NetQuality skips the AS17676 hand-off at the mainland entry.
		if hasASN(hops[firstChina], "17676") && firstChina+1 < len(hops) {
			firstChina++
		}
		entry := hops[firstChina]
		switch {
		// NetQuality normalizes 59.43.* to AS4809 before testing the entry ASN.
		case inIPv4Prefix(entry, 59, 43) || hasASN(entry, "4809"):
			if routeHopTTL(entry, firstChina) > 1 {
				if hasASNIn(hops, "23764") {
					return "CTGGIA"
				}
				return "CN2GIA"
			}
			for _, later := range hops[firstChina+1:] {
				if inIPv4Prefix(later, 59, 43) || hasASN(later, "4809") || hasASN(later, "23764") {
					continue
				}
				if inIPv4Prefix(later, 202, 97) {
					return "CN2GT"
				}
				break
			}
			return "CN2GIA"
		case hasASN(entry, "4134"):
			return "163"
		case hasASN(entry, "4837"):
			if firstChina > 0 && hasASN(hops[firstChina-1], "10099") {
				return "10099"
			}
			return "4837"
		case hasASN(entry, "58453"):
			return "CMI"
		case hasASN(entry, "58807"):
			return "CMIN2"
		case hasASN(entry, "9808"):
			if hasASNIn(hops, "58807") {
				return "CMIN2"
			}
			return "CMI"
		case hasASN(entry, "9929"):
			return "9929"
		case hasASN(entry, "10099"):
			if hasASNIn(hops, "9929") {
				return "9929"
			}
			return "10099"
		case hasASN(entry, "23764"):
			return "CTGGIA"
		case hasASN(entry, "4538"):
			return "CERNET"
		case hasASN(entry, "7497"):
			return "CSTNET"
		}
	}
	// Keep NetQuality's fallback order. These generic labels do not imply a
	// confirmed mainland entry or a premium CN2 tier.
	switch {
	case hasASNIn(hops, "58807"):
		return "CMIN2"
	case hasASNIn(hops, "9929"):
		return "9929"
	case hasASNIn(hops, "10099"):
		return "10099"
	case hasCN2Hop(hops):
		return "CN2"
	case hasASNIn(hops, "9808"):
		return "CMI"
	case hasASNIn(hops, "4134"):
		return "163"
	case hasASNIn(hops, "4837"):
		return "4837"
	default:
		return ""
	}
}

func hasCN2Hop(hops []routeHop) bool {
	for _, hop := range hops {
		if inIPv4Prefix(hop, 59, 43) || hasASN(hop, "4809") {
			return true
		}
	}
	return false
}

func routeHopTTL(hop routeHop, index int) int {
	if hop.ttl > 0 {
		return hop.ttl
	}
	return index + 1 // Synthetic tests and older in-process callers.
}

func cymruDNSName(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return strings.Join([]string{
			strconv.Itoa(int(v4[3])), strconv.Itoa(int(v4[2])),
			strconv.Itoa(int(v4[1])), strconv.Itoa(int(v4[0])),
		}, ".") + ".origin.asn.cymru.com"
	}
	var nibbles []string
	for index := len(ip.To16()) - 1; index >= 0; index-- {
		value := ip[index]
		nibbles = append(nibbles, strconv.FormatUint(uint64(value&15), 16), strconv.FormatUint(uint64(value>>4), 16))
	}
	return strings.Join(nibbles, ".") + ".origin6.asn.cymru.com"
}

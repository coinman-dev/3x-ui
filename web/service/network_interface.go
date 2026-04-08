package service

import (
	"bufio"
	"os"
	"runtime"
	"strings"
)

const ipv6ZeroRoute = "00000000000000000000000000000000"

// detectDefaultRouteInterfaces returns Linux default-route interfaces for IPv4 and IPv6.
// Empty string means the route was not detected for that IP family.
func detectDefaultRouteInterfaces() (string, string) {
	if runtime.GOOS != "linux" {
		return "", ""
	}
	return detectDefaultRouteInterfaceIPv4(), detectDefaultRouteInterfaceIPv6()
}

func detectDefaultRouteInterfaceIPv4() string {
	file, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "Iface") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}
		// Destination + mask all zeros means default route.
		if fields[1] == "00000000" && fields[7] == "00000000" {
			return fields[0]
		}
	}

	return ""
}

func detectDefaultRouteInterfaceIPv6() string {
	file, err := os.Open("/proc/net/ipv6_route")
	if err != nil {
		return ""
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 10 {
			continue
		}
		// Destination all zeros + prefix length 0 means default route.
		if fields[0] == ipv6ZeroRoute && fields[1] == "00000000" {
			return fields[9]
		}
	}

	return ""
}

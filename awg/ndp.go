package awg

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/mhsanaei/3x-ui/v2/logger"
)

const ndppdConfigPath = "/etc/ndppd.conf"

// GenerateNdppdConfig creates ndppd.conf content for proxying IPv6 NDP
// between the external interface and the AWG tunnel interface.
func GenerateNdppdConfig(externalIface, tunnelIface, ipv6Pool string) string {
	return fmt.Sprintf(`route-ttl 30000

proxy %s {
    router yes
    timeout 500
    ttl 30000
    rule %s {
        iface %s
    }
}
`, externalIface, ipv6Pool, tunnelIface)
}

// ApplyNdppdConfig writes the config and restarts ndppd.
func ApplyNdppdConfig(externalIface, tunnelIface, ipv6Pool string) error {
	config := GenerateNdppdConfig(externalIface, tunnelIface, ipv6Pool)

	if err := os.WriteFile(ndppdConfigPath, []byte(config), 0644); err != nil {
		return fmt.Errorf("write ndppd config: %w", err)
	}

	// Try systemctl restart first, fall back to service command
	if err := exec.Command("systemctl", "restart", "ndppd").Run(); err != nil {
		logger.Warning("systemctl restart ndppd failed, trying service command:", err)
		if err2 := exec.Command("service", "ndppd", "restart").Run(); err2 != nil {
			return fmt.Errorf("restart ndppd: %w", err2)
		}
	}

	logger.Info("ndppd config applied for pool", ipv6Pool)
	return nil
}

// StopNdppd stops the ndppd service.
func StopNdppd() {
	_ = exec.Command("systemctl", "stop", "ndppd").Run()
}

// AddProxyNDP adds a single IPv6 NDP proxy entry (fallback method without ndppd).
func AddProxyNDP(ipv6 string, externalIface string) error {
	ip := stripMask(ipv6)
	cmd := exec.Command("ip", "-6", "neigh", "add", "proxy", ip, "dev", externalIface)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Ignore "File exists" error
		if strings.Contains(string(output), "File exists") {
			return nil
		}
		return fmt.Errorf("add NDP proxy for %s: %s: %w", ip, string(output), err)
	}
	return nil
}

// RemoveProxyNDP removes a single IPv6 NDP proxy entry.
func RemoveProxyNDP(ipv6 string, externalIface string) error {
	ip := stripMask(ipv6)
	cmd := exec.Command("ip", "-6", "neigh", "del", "proxy", ip, "dev", externalIface)
	output, err := cmd.CombinedOutput()
	if err != nil {
		if strings.Contains(string(output), "No such") {
			return nil
		}
		return fmt.Errorf("remove NDP proxy for %s: %s: %w", ip, string(output), err)
	}
	return nil
}

// IsNdppdInstalled checks if ndppd is available on the system.
func IsNdppdInstalled() bool {
	_, err := exec.LookPath("ndppd")
	return err == nil
}

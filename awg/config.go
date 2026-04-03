package awg

import (
	"fmt"
	"strings"

	"github.com/mhsanaei/3x-ui/v2/database/model"
)

// GenerateServerConfig builds the awg0.conf content from server settings and clients.
func GenerateServerConfig(server *model.AwgServer, clients []model.AwgClient) string {
	var b strings.Builder

	b.WriteString("[Interface]\n")
	b.WriteString(fmt.Sprintf("PrivateKey = %s\n", server.PrivateKey))

	// Address line
	addresses := []string{server.IPv4Address}
	if server.IPv6Enabled && server.IPv6Address != "" {
		addresses = append(addresses, server.IPv6Address)
	}
	b.WriteString(fmt.Sprintf("Address = %s\n", strings.Join(addresses, ", ")))

	b.WriteString(fmt.Sprintf("ListenPort = %d\n", server.ListenPort))

	if server.MTU > 0 {
		b.WriteString(fmt.Sprintf("MTU = %d\n", server.MTU))
	}

	// AmneziaWG obfuscation parameters
	b.WriteString(fmt.Sprintf("Jc = %d\n", server.Jc))
	b.WriteString(fmt.Sprintf("Jmin = %d\n", server.Jmin))
	b.WriteString(fmt.Sprintf("Jmax = %d\n", server.Jmax))
	b.WriteString(fmt.Sprintf("S1 = %d\n", server.S1))
	b.WriteString(fmt.Sprintf("S2 = %d\n", server.S2))
	b.WriteString(fmt.Sprintf("H1 = %d\n", server.H1))
	b.WriteString(fmt.Sprintf("H2 = %d\n", server.H2))
	b.WriteString(fmt.Sprintf("H3 = %d\n", server.H3))
	b.WriteString(fmt.Sprintf("H4 = %d\n", server.H4))

	// PostUp / PostDown
	postUp := server.PostUp
	if postUp == "" {
		postUp = GenerateDefaultPostUp(server)
	}
	postDown := server.PostDown
	if postDown == "" {
		postDown = GenerateDefaultPostDown(server)
	}
	if postUp != "" {
		b.WriteString(fmt.Sprintf("PostUp = %s\n", postUp))
	}
	if postDown != "" {
		b.WriteString(fmt.Sprintf("PostDown = %s\n", postDown))
	}

	// Peers
	for _, c := range clients {
		if !c.Enable {
			continue
		}
		b.WriteString("\n[Peer]\n")
		b.WriteString(fmt.Sprintf("# %s\n", c.Name))
		b.WriteString(fmt.Sprintf("PublicKey = %s\n", c.PublicKey))
		if c.PresharedKey != "" {
			b.WriteString(fmt.Sprintf("PresharedKey = %s\n", c.PresharedKey))
		}
		b.WriteString(fmt.Sprintf("AllowedIPs = %s\n", c.AllowedIPs))
	}

	return b.String()
}

// GenerateClientConfig builds a client .conf file content.
func GenerateClientConfig(server *model.AwgServer, client *model.AwgClient) string {
	var b strings.Builder

	b.WriteString("[Interface]\n")
	b.WriteString(fmt.Sprintf("PrivateKey = %s\n", client.PrivateKey))

	// Client addresses
	addresses := []string{client.IPv4Address}
	if server.IPv6Enabled && client.IPv6Address != "" {
		addresses = append(addresses, client.IPv6Address)
	}
	b.WriteString(fmt.Sprintf("Address = %s\n", strings.Join(addresses, ", ")))

	if server.DNS != "" {
		b.WriteString(fmt.Sprintf("DNS = %s\n", server.DNS))
	}

	if server.MTU > 0 {
		b.WriteString(fmt.Sprintf("MTU = %d\n", server.MTU))
	}

	// AmneziaWG obfuscation — client must have same params as server
	b.WriteString(fmt.Sprintf("Jc = %d\n", server.Jc))
	b.WriteString(fmt.Sprintf("Jmin = %d\n", server.Jmin))
	b.WriteString(fmt.Sprintf("Jmax = %d\n", server.Jmax))
	b.WriteString(fmt.Sprintf("S1 = %d\n", server.S1))
	b.WriteString(fmt.Sprintf("S2 = %d\n", server.S2))
	b.WriteString(fmt.Sprintf("H1 = %d\n", server.H1))
	b.WriteString(fmt.Sprintf("H2 = %d\n", server.H2))
	b.WriteString(fmt.Sprintf("H3 = %d\n", server.H3))
	b.WriteString(fmt.Sprintf("H4 = %d\n", server.H4))

	// Server peer
	b.WriteString("\n[Peer]\n")
	b.WriteString(fmt.Sprintf("PublicKey = %s\n", server.PublicKey))
	if client.PresharedKey != "" {
		b.WriteString(fmt.Sprintf("PresharedKey = %s\n", client.PresharedKey))
	}

	endpoint := server.Endpoint
	if endpoint != "" {
		if !strings.Contains(endpoint, ":") {
			endpoint = fmt.Sprintf("%s:%d", endpoint, server.ListenPort)
		}
		b.WriteString(fmt.Sprintf("Endpoint = %s\n", endpoint))
	}

	allowedIPs := client.ClientAllowedIPs
	if allowedIPs == "" {
		allowedIPs = "0.0.0.0/0, ::/0"
	}
	b.WriteString(fmt.Sprintf("AllowedIPs = %s\n", allowedIPs))

	if client.PersistentKeepalive > 0 {
		b.WriteString(fmt.Sprintf("PersistentKeepalive = %d\n", client.PersistentKeepalive))
	}

	return b.String()
}

// ipv6Iface returns the external interface for IPv6 operations,
// falling back to the IPv4 external interface if not set separately.
func ipv6Iface(server *model.AwgServer) string {
	if server.IPv6ExternalInterface != "" {
		return server.IPv6ExternalInterface
	}
	if server.ExternalInterface != "" {
		return server.ExternalInterface
	}
	return "eth0"
}

// GenerateDefaultPostUp creates default iptables rules for the server.
func GenerateDefaultPostUp(server *model.AwgServer) string {
	iface := server.ExternalInterface
	if iface == "" {
		iface = "eth0"
	}
	name := server.InterfaceName
	if name == "" {
		name = "awg0"
	}

	parts := []string{
		fmt.Sprintf("iptables -t nat -A POSTROUTING -s %s -o %s -j MASQUERADE", server.IPv4Pool, iface),
		fmt.Sprintf("iptables -A FORWARD -i %s -j ACCEPT", name),
		fmt.Sprintf("iptables -A FORWARD -o %s -j ACCEPT", name),
	}

	if server.IPv6Enabled {
		iface6 := ipv6Iface(server)
		// No NAT66 — direct routing with forwarding
		parts = append(parts,
			fmt.Sprintf("ip6tables -A FORWARD -i %s -j ACCEPT", name),
			fmt.Sprintf("ip6tables -A FORWARD -o %s -j ACCEPT", name),
			fmt.Sprintf("ip6tables -A FORWARD -i %s -o %s -j ACCEPT", iface6, name),
			"sysctl -w net.ipv6.conf.all.forwarding=1",
		)
	}
	parts = append(parts, "sysctl -w net.ipv4.ip_forward=1")

	return strings.Join(parts, "; ")
}

// GenerateDefaultPostDown creates cleanup rules matching PostUp.
func GenerateDefaultPostDown(server *model.AwgServer) string {
	iface := server.ExternalInterface
	if iface == "" {
		iface = "eth0"
	}
	name := server.InterfaceName
	if name == "" {
		name = "awg0"
	}

	parts := []string{
		fmt.Sprintf("iptables -t nat -D POSTROUTING -s %s -o %s -j MASQUERADE", server.IPv4Pool, iface),
		fmt.Sprintf("iptables -D FORWARD -i %s -j ACCEPT", name),
		fmt.Sprintf("iptables -D FORWARD -o %s -j ACCEPT", name),
	}

	if server.IPv6Enabled {
		iface6 := ipv6Iface(server)
		parts = append(parts,
			fmt.Sprintf("ip6tables -D FORWARD -i %s -j ACCEPT", name),
			fmt.Sprintf("ip6tables -D FORWARD -o %s -j ACCEPT", name),
			fmt.Sprintf("ip6tables -D FORWARD -i %s -o %s -j ACCEPT", iface6, name),
		)
	}

	return strings.Join(parts, "; ")
}

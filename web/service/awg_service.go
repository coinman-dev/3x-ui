package service

import (
	"fmt"
	"time"

	"github.com/mhsanaei/3x-ui/v2/awg"
	"github.com/mhsanaei/3x-ui/v2/database"
	"github.com/mhsanaei/3x-ui/v2/database/model"
	"github.com/mhsanaei/3x-ui/v2/logger"
)

type AwgService struct{}

// GetServer returns the AWG server config, creating a default one if none exists.
func (s *AwgService) GetServer() (*model.AwgServer, error) {
	db := database.GetDB()
	var server model.AwgServer
	err := db.FirstOrCreate(&server).Error
	if err != nil {
		return nil, err
	}

	// Generate keys if missing
	if server.PrivateKey == "" {
		priv, pub, err := awg.GenerateKeyPair()
		if err != nil {
			return nil, fmt.Errorf("generate server keys: %w", err)
		}
		server.PrivateKey = priv
		server.PublicKey = pub
		if err := db.Save(&server).Error; err != nil {
			return nil, err
		}
	}

	return &server, nil
}

// SaveServer saves server settings and optionally applies them to the OS.
func (s *AwgService) SaveServer(server *model.AwgServer) error {
	db := database.GetDB()
	server.UpdatedAt = time.Now().UnixMilli()
	if err := db.Save(server).Error; err != nil {
		return err
	}

	if server.Enable {
		return s.applyServerConfig(server)
	}
	return nil
}

// ToggleServer enables or disables the AWG interface.
func (s *AwgService) ToggleServer(enable bool) error {
	server, err := s.GetServer()
	if err != nil {
		return err
	}

	server.Enable = enable
	db := database.GetDB()
	if err := db.Save(server).Error; err != nil {
		return err
	}

	if enable {
		return s.applyServerConfig(server)
	}
	// Disable
	awg.StopNdppd()
	return awg.InterfaceDown(server.InterfaceName)
}

// GetServerStatus returns basic status info.
type AwgStatus struct {
	Running      bool   `json:"running"`
	AwgInstalled bool   `json:"awgInstalled"`
	AwgVersion   string `json:"awgVersion"`
}

func (s *AwgService) GetServerStatus() *AwgStatus {
	server, _ := s.GetServer()
	ifaceName := "awg0"
	if server != nil {
		ifaceName = server.InterfaceName
	}
	return &AwgStatus{
		Running:      awg.IsInterfaceUp(ifaceName),
		AwgInstalled: awg.IsAwgInstalled(),
		AwgVersion:   awg.GetAwgVersion(),
	}
}

// --- Clients ---

// GetClients returns all clients for the server, enriched with live traffic stats.
func (s *AwgService) GetClients() ([]model.AwgClient, error) {
	db := database.GetDB()
	var clients []model.AwgClient
	if err := db.Order("id asc").Find(&clients).Error; err != nil {
		return nil, err
	}
	return clients, nil
}

// GetClient returns a single client by ID.
func (s *AwgService) GetClient(id int) (*model.AwgClient, error) {
	db := database.GetDB()
	var client model.AwgClient
	if err := db.First(&client, id).Error; err != nil {
		return nil, err
	}
	return &client, nil
}

// AddClient creates a new client with auto-generated keys and allocated IPs.
func (s *AwgService) AddClient(client *model.AwgClient) error {
	server, err := s.GetServer()
	if err != nil {
		return err
	}

	// Generate keys
	priv, pub, err := awg.GenerateKeyPair()
	if err != nil {
		return fmt.Errorf("generate client keys: %w", err)
	}
	client.PrivateKey = priv
	client.PublicKey = pub

	psk, err := awg.GeneratePresharedKey()
	if err != nil {
		return fmt.Errorf("generate PSK: %w", err)
	}
	client.PresharedKey = psk

	// Allocate IPv4
	existingClients, err := s.GetClients()
	if err != nil {
		return err
	}
	usedIPv4 := make([]string, 0, len(existingClients))
	usedIPv6 := make([]string, 0, len(existingClients))
	for _, c := range existingClients {
		usedIPv4 = append(usedIPv4, c.IPv4Address)
		if c.IPv6Address != "" {
			usedIPv6 = append(usedIPv6, c.IPv6Address)
		}
	}

	ipv4, err := awg.AllocateIPv4(server.IPv4Pool, server.IPv4Address, usedIPv4)
	if err != nil {
		return fmt.Errorf("allocate IPv4: %w", err)
	}
	client.IPv4Address = ipv4

	// Allocate IPv6 if enabled
	if server.IPv6Enabled && server.IPv6Pool != "" {
		ipv6, err := awg.AllocateIPv6(server.IPv6Pool, server.IPv6Address, usedIPv6)
		if err != nil {
			return fmt.Errorf("allocate IPv6: %w", err)
		}
		client.IPv6Address = ipv6
	}

	// Build server-side AllowedIPs
	allowedIPs := client.IPv4Address
	if client.IPv6Address != "" {
		allowedIPs += ", " + client.IPv6Address
	}
	client.AllowedIPs = allowedIPs

	if client.ClientAllowedIPs == "" {
		client.ClientAllowedIPs = "0.0.0.0/0, ::/0"
	}

	client.ServerId = server.Id
	client.CreatedAt = time.Now().UnixMilli()

	db := database.GetDB()
	if err := db.Create(client).Error; err != nil {
		return err
	}

	// Apply to running server
	if server.Enable {
		if err := s.applyServerConfig(server); err != nil {
			logger.Warning("Failed to apply AWG config after adding client:", err)
		}
	}

	return nil
}

// UpdateClient updates an existing client.
func (s *AwgService) UpdateClient(client *model.AwgClient) error {
	db := database.GetDB()
	client.UpdatedAt = time.Now().UnixMilli()
	if err := db.Save(client).Error; err != nil {
		return err
	}

	server, err := s.GetServer()
	if err != nil {
		return err
	}
	if server.Enable {
		if err := s.applyServerConfig(server); err != nil {
			logger.Warning("Failed to apply AWG config after updating client:", err)
		}
	}
	return nil
}

// DeleteClient removes a client and cleans up NDP proxy if needed.
func (s *AwgService) DeleteClient(id int) error {
	client, err := s.GetClient(id)
	if err != nil {
		return err
	}

	server, err := s.GetServer()
	if err != nil {
		return err
	}

	// Remove NDP proxy entry
	if server.IPv6Enabled && client.IPv6Address != "" {
		_ = awg.RemoveProxyNDP(client.IPv6Address, server.ExternalInterface)
	}

	db := database.GetDB()
	if err := db.Delete(&model.AwgClient{}, id).Error; err != nil {
		return err
	}

	if server.Enable {
		if err := s.applyServerConfig(server); err != nil {
			logger.Warning("Failed to apply AWG config after deleting client:", err)
		}
	}
	return nil
}

// ToggleClient enables or disables a client.
func (s *AwgService) ToggleClient(id int, enable bool) error {
	client, err := s.GetClient(id)
	if err != nil {
		return err
	}
	client.Enable = enable
	return s.UpdateClient(client)
}

// GetClientConfig returns the text content of a client .conf file.
func (s *AwgService) GetClientConfig(id int) (string, error) {
	client, err := s.GetClient(id)
	if err != nil {
		return "", err
	}
	server, err := s.GetServer()
	if err != nil {
		return "", err
	}
	return awg.GenerateClientConfig(server, client), nil
}

// ResetClientTraffic resets upload/download counters for a client.
func (s *AwgService) ResetClientTraffic(id int) error {
	db := database.GetDB()
	return db.Model(&model.AwgClient{}).Where("id = ?", id).Updates(map[string]any{
		"upload":   0,
		"download": 0,
	}).Error
}

// UpdateTrafficStats reads live peer stats and updates the database.
func (s *AwgService) UpdateTrafficStats() {
	server, err := s.GetServer()
	if err != nil || !server.Enable {
		return
	}

	if !awg.IsInterfaceUp(server.InterfaceName) {
		return
	}

	peers, err := awg.GetPeerStats(server.InterfaceName)
	if err != nil {
		return
	}

	db := database.GetDB()
	var clients []model.AwgClient
	if err := db.Find(&clients).Error; err != nil {
		return
	}

	// Build pubkey -> client map
	clientMap := make(map[string]*model.AwgClient)
	for i := range clients {
		clientMap[clients[i].PublicKey] = &clients[i]
	}

	for _, peer := range peers {
		client, ok := clientMap[peer.PublicKey]
		if !ok {
			continue
		}

		// Calculate delta (peer stats are cumulative since interface up)
		newUp := peer.TransferTx // from server perspective: TX to peer = client's upload perspective is reversed
		newDown := peer.TransferRx

		// Update only if there's actual traffic change
		// awg show returns cumulative stats, so we store the raw values
		if newUp != client.Upload || newDown != client.Download {
			allTimeDelta := int64(0)
			if newUp > client.Upload {
				allTimeDelta += newUp - client.Upload
			}
			if newDown > client.Download {
				allTimeDelta += newDown - client.Download
			}

			db.Model(client).Updates(map[string]any{
				"upload":   newUp,
				"download": newDown,
				"all_time": client.AllTime + allTimeDelta,
			})
		}
	}
}

// applyServerConfig regenerates the config file and applies it.
func (s *AwgService) applyServerConfig(server *model.AwgServer) error {
	clients, err := s.GetClients()
	if err != nil {
		return err
	}

	configContent := awg.GenerateServerConfig(server, clients)

	if err := awg.WriteServerConfig(server.InterfaceName, configContent); err != nil {
		return err
	}

	// Apply NDP proxy for IPv6
	if server.IPv6Enabled && server.IPv6Pool != "" {
		if awg.IsNdppdInstalled() {
			if err := awg.ApplyNdppdConfig(server.ExternalInterface, server.InterfaceName, server.IPv6Pool); err != nil {
				logger.Warning("Failed to apply ndppd config:", err)
				// Fallback to per-client NDP proxy
				s.applyManualNDP(server, clients)
			}
		} else {
			s.applyManualNDP(server, clients)
		}
	}

	// Sync or restart interface
	if awg.IsInterfaceUp(server.InterfaceName) {
		return awg.SyncConfig(server.InterfaceName)
	}
	return awg.InterfaceUp(server.InterfaceName)
}

// applyManualNDP adds per-client NDP proxy entries.
func (s *AwgService) applyManualNDP(server *model.AwgServer, clients []model.AwgClient) {
	for _, c := range clients {
		if c.Enable && c.IPv6Address != "" {
			if err := awg.AddProxyNDP(c.IPv6Address, server.ExternalInterface); err != nil {
				logger.Warning("Failed to add NDP proxy for", c.IPv6Address, ":", err)
			}
		}
	}
}

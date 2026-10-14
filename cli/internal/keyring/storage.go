package keyring

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/zalando/go-keyring"
)

const (
	ServiceName = "qsync"
	DefaultKey  = "default_token"
)

type DeviceInfo struct {
	Name string `json:"name"`
	IP   string `json:"ip"`
	Port int    `json:"port"`
}

type Config struct {
	LastServerName string                `json:"last_server_name"`
	LastServerIP   string                `json:"last_server_ip"`
	LastServerPort int                   `json:"last_server_port"`
	PairedDevices  map[string]DeviceInfo `json:"paired_devices,omitempty"`
}

func getConfigPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "qsync-client")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "client_config.json"), nil
}

func SaveConfig(cfg Config) error {
	path, err := getConfigPath()
	if err != nil {
		return err
	}
	if cfg.PairedDevices == nil {
		cfg.PairedDevices = make(map[string]DeviceInfo)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func LoadConfig() (Config, error) {
	var cfg Config
	path, err := getConfigPath()
	if err != nil {
		return cfg, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(data, &cfg)
	if cfg.PairedDevices == nil {
		cfg.PairedDevices = make(map[string]DeviceInfo)
	}
	return cfg, err
}

func NormalizeServerKey(name string) string {
	if name == "" {
		return DefaultKey
	}
	// Replace non-alphanumeric chars with _
	var clean []rune
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			clean = append(clean, r)
		} else {
			clean = append(clean, '_')
		}
	}
	return "dev_" + string(clean)
}

func SaveToken(serverID, token string) error {
	if serverID == "" {
		serverID = DefaultKey
	}
	return keyring.Set(ServiceName, serverID, token)
}

func GetToken(serverID string) (string, error) {
	if serverID == "" {
		serverID = DefaultKey
	}
	token, err := keyring.Get(ServiceName, serverID)
	if err != nil {
		// Fallback to default if specific key not found
		if serverID != DefaultKey {
			return keyring.Get(ServiceName, DefaultKey)
		}
		return "", fmt.Errorf("failed to retrieve token: %w", err)
	}
	return token, nil
}

func DeleteToken(serverID string) error {
	if serverID == "" {
		serverID = DefaultKey
	}
	return keyring.Delete(ServiceName, serverID)
}

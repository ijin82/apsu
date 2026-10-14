package keyring

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultKey = "default_token"
)

type DeviceInfo struct {
	Name  string `json:"name"`
	IP    string `json:"ip"`
	Port  int    `json:"port"`
	Token string `json:"token,omitempty"`
}

type Config struct {
	DefaultToken   string                `json:"default_token,omitempty"`
	Tokens         map[string]string     `json:"tokens,omitempty"` // key: deviceKey or name
	LastServerName string                `json:"last_server_name"`
	LastServerIP   string                `json:"last_server_ip"`
	LastServerPort int                   `json:"last_server_port"`
	PairedDevices  map[string]DeviceInfo `json:"paired_devices,omitempty"`
}

func getConfigPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(home, ".config")
	}
	dir := filepath.Join(configDir, "qsync")
	if err := os.MkdirAll(dir, 0700); err != nil {
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
	if cfg.Tokens == nil {
		cfg.Tokens = make(map[string]string)
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
		if cfg.PairedDevices == nil {
			cfg.PairedDevices = make(map[string]DeviceInfo)
		}
		if cfg.Tokens == nil {
			cfg.Tokens = make(map[string]string)
		}
		return cfg, err
	}

	err = json.Unmarshal(data, &cfg)
	if cfg.PairedDevices == nil {
		cfg.PairedDevices = make(map[string]DeviceInfo)
	}
	if cfg.Tokens == nil {
		cfg.Tokens = make(map[string]string)
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
	cfg, _ := LoadConfig()
	if cfg.Tokens == nil {
		cfg.Tokens = make(map[string]string)
	}
	cfg.Tokens[serverID] = token
	if serverID == DefaultKey {
		cfg.DefaultToken = token
	}
	return SaveConfig(cfg)
}

func GetToken(serverID string) (string, error) {
	if serverID == "" {
		serverID = DefaultKey
	}
	cfg, err := LoadConfig()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	if token, ok := cfg.Tokens[serverID]; ok && token != "" {
		return token, nil
	}
	// Check inside PairedDevices map
	for name, dev := range cfg.PairedDevices {
		if name == serverID || NormalizeServerKey(name) == serverID {
			if dev.Token != "" {
				return dev.Token, nil
			}
		}
	}
	// Fallback to default token
	if serverID != DefaultKey {
		if token, ok := cfg.Tokens[DefaultKey]; ok && token != "" {
			return token, nil
		}
		if cfg.DefaultToken != "" {
			return cfg.DefaultToken, nil
		}
	}

	return "", fmt.Errorf("token not found for server: %s", serverID)
}

func DeleteToken(serverID string) error {
	if serverID == "" {
		serverID = DefaultKey
	}
	cfg, err := LoadConfig()
	if err == nil {
		delete(cfg.Tokens, serverID)
		if serverID == DefaultKey {
			cfg.DefaultToken = ""
		}
		_ = SaveConfig(cfg)
	}
	return nil
}

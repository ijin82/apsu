package keyring

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	oskeyring "github.com/zalando/go-keyring"
)

const (
	ServiceName = "apsu"
	DefaultKey  = "default_token"
)

var configMu sync.Mutex

type DeviceInfo struct {
	Name            string `json:"name"`
	IP              string `json:"ip"`
	Port            int    `json:"port"`
	TLSPort         int    `json:"tls_port,omitempty"`
	CertFingerprint string `json:"cert_fingerprint,omitempty"`
	Token           string `json:"token,omitempty"`
}

type Config struct {
	DefaultToken   string                       `json:"default_token,omitempty"`
	Tokens         map[string]string            `json:"tokens,omitempty"` // key: deviceKey or name
	LastServerName string                       `json:"last_server_name"`
	LastServerIP   string                       `json:"last_server_ip"`
	LastServerPort int                          `json:"last_server_port"`
	PairedDevices  map[string]DeviceInfo        `json:"paired_devices,omitempty"`
	SyncMappings   map[string]map[string]string `json:"sync_mappings,omitempty"` // key: deviceName -> folderName -> localPath
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
	dir := filepath.Join(configDir, "apsu")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	targetConfig := filepath.Join(dir, "client_config.json")
	if _, err := os.Stat(targetConfig); os.IsNotExist(err) {
		oldConfig := filepath.Join(configDir, "qsync", "client_config.json")
		if data, err := os.ReadFile(oldConfig); err == nil {
			_ = os.WriteFile(targetConfig, data, 0600)
		}
	}
	return targetConfig, nil
}

func saveConfigLocked(cfg Config) error {
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
	if cfg.SyncMappings == nil {
		cfg.SyncMappings = make(map[string]map[string]string)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write: write to temp file then rename to avoid partial reads by other threads
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmpPath, path)
}

func loadConfigLocked() (Config, error) {
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

func SaveConfig(cfg Config) error {
	configMu.Lock()
	defer configMu.Unlock()
	return saveConfigLocked(cfg)
}

func LoadConfig() (Config, error) {
	configMu.Lock()
	defer configMu.Unlock()
	return loadConfigLocked()
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
	// Save to system keyring if available
	keyringErr := oskeyring.Set(ServiceName, serverID, token)

	configMu.Lock()
	defer configMu.Unlock()

	cfg, _ := loadConfigLocked()
	if cfg.Tokens == nil {
		cfg.Tokens = make(map[string]string)
	}

	if keyringErr != nil {
		// Keyring unavailable (e.g. headless/test environment): save as fallback
		cfg.Tokens[serverID] = token
		if serverID == DefaultKey {
			cfg.DefaultToken = token
		}
	} else {
		// Keyring succeeded: do NOT leak plaintext token to config file
		delete(cfg.Tokens, serverID)
		if serverID == DefaultKey {
			cfg.DefaultToken = ""
		}
	}
	return saveConfigLocked(cfg)
}

func GetToken(serverID string) (string, error) {
	if serverID == "" {
		serverID = DefaultKey
	}
	// 1. Try system keyring first
	if tok, err := oskeyring.Get(ServiceName, serverID); err == nil && tok != "" {
		return tok, nil
	}

	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigLocked()
	if err != nil {
		return "", fmt.Errorf("failed to load config: %w", err)
	}

	if serverID == DefaultKey {
		if token, ok := cfg.Tokens[DefaultKey]; ok && token != "" {
			return token, nil
		}
		if cfg.DefaultToken != "" {
			return cfg.DefaultToken, nil
		}
		return "", fmt.Errorf("default token not found")
	}

	if token, ok := cfg.Tokens[serverID]; ok && token != "" {
		return token, nil
	}
	// Check inside PairedDevices map
	for name, dev := range cfg.PairedDevices {
		if name == serverID || NormalizeServerKey(name) == serverID {
			if tok, err := oskeyring.Get(ServiceName, name); err == nil && tok != "" {
				return tok, nil
			}
			if dev.Token != "" {
				return dev.Token, nil
			}
		}
	}

	return "", fmt.Errorf("token not found for server: %s", serverID)
}

func DeleteToken(serverID string) error {
	if serverID == "" {
		serverID = DefaultKey
	}
	_ = oskeyring.Delete(ServiceName, serverID)

	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigLocked()
	if err == nil {
		delete(cfg.Tokens, serverID)
		if serverID == DefaultKey {
			cfg.DefaultToken = ""
		}
		_ = saveConfigLocked(cfg)
	}
	return nil
}

// UpdatePairedDevice updates or renames a paired device in config when tokens match
func UpdatePairedDevice(name, ip string, port int, token string) error {
	return UpdatePairedDeviceSecure(name, ip, port, 0, "", token)
}

// UpdatePairedDeviceSecure updates device info with TLS configuration and certificate fingerprint
func UpdatePairedDeviceSecure(name, ip string, port, tlsPort int, certFP, token string) error {
	if name == "" {
		return nil
	}
	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigLocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if cfg.PairedDevices == nil {
		cfg.PairedDevices = make(map[string]DeviceInfo)
	}
	if cfg.Tokens == nil {
		cfg.Tokens = make(map[string]string)
	}

	// Find if this device was previously stored under a different name (matching IP or matching token)
	var oldNames []string
	for k, v := range cfg.PairedDevices {
		if k != name && ((ip != "" && v.IP == ip) || (token != "" && v.Token == token)) {
			oldNames = append(oldNames, k)
		}
	}

	for _, oldName := range oldNames {
		delete(cfg.PairedDevices, oldName)
		delete(cfg.Tokens, NormalizeServerKey(oldName))
		_ = oskeyring.Delete(ServiceName, oldName)
		_ = oskeyring.Delete(ServiceName, NormalizeServerKey(oldName))
		if cfg.LastServerName == oldName {
			cfg.LastServerName = name
		}
		if cfg.SyncMappings != nil && cfg.SyncMappings[oldName] != nil {
			if cfg.SyncMappings[name] == nil {
				cfg.SyncMappings[name] = make(map[string]string)
			}
			for fName, lPath := range cfg.SyncMappings[oldName] {
				if _, exists := cfg.SyncMappings[name][fName]; !exists {
					cfg.SyncMappings[name][fName] = lPath
				}
			}
			delete(cfg.SyncMappings, oldName)
		}
	}

	newKey := NormalizeServerKey(name)
	keyringSuccess := false
	if token != "" {
		err1 := oskeyring.Set(ServiceName, newKey, token)
		err2 := oskeyring.Set(ServiceName, name, token)
		err3 := oskeyring.Set(ServiceName, DefaultKey, token)
		if err1 == nil && err2 == nil && err3 == nil {
			keyringSuccess = true
		}
	}

	savedToken := ""
	if !keyringSuccess && token != "" {
		// Fallback: save to json only if keyring is unavailable
		savedToken = token
		cfg.Tokens[newKey] = token
		cfg.Tokens[DefaultKey] = token
		cfg.DefaultToken = token
	} else {
		// Keyring succeeded: do not store token in json
		delete(cfg.Tokens, newKey)
		delete(cfg.Tokens, DefaultKey)
		cfg.DefaultToken = ""
	}

	cfg.PairedDevices[name] = DeviceInfo{
		Name:            name,
		IP:              ip,
		Port:            port,
		TLSPort:         tlsPort,
		CertFingerprint: certFP,
		Token:           savedToken,
	}

	if cfg.LastServerIP == ip || cfg.LastServerName == "" {
		cfg.LastServerName = name
		cfg.LastServerIP = ip
		cfg.LastServerPort = port
	}

	return saveConfigLocked(cfg)
}

// EnsureSyncFoldersInConfig checks the phone's syncable folders for deviceName.
// If any folder is not yet listed in cfg.SyncMappings[deviceName], it adds it with an empty path ("")
// so that a user can easily open client_config.json and fill in the local source directory manually.
func EnsureSyncFoldersInConfig(deviceName string, remoteSyncFolders []string) error {
	if deviceName == "" || len(remoteSyncFolders) == 0 {
		return nil
	}
	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigLocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if cfg.SyncMappings == nil {
		cfg.SyncMappings = make(map[string]map[string]string)
	}
	if cfg.SyncMappings[deviceName] == nil {
		cfg.SyncMappings[deviceName] = make(map[string]string)
	}

	changed := false
	for _, folder := range remoteSyncFolders {
		if _, exists := cfg.SyncMappings[deviceName][folder]; !exists {
			cfg.SyncMappings[deviceName][folder] = ""
			changed = true
		}
	}

	if changed {
		return saveConfigLocked(cfg)
	}
	return nil
}

// GetSyncMappings returns the map of remoteFolderName -> localPath for a device.
func GetSyncMappings(deviceName string) map[string]string {
	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigLocked()
	if err != nil || cfg.SyncMappings == nil {
		return make(map[string]string)
	}
	if m, ok := cfg.SyncMappings[deviceName]; ok {
		res := make(map[string]string)
		for k, v := range m {
			res[k] = v
		}
		return res
	}
	return make(map[string]string)
}

// SetSyncMapping saves a local folder path mapping for a specific device and remote folder.
func SetSyncMapping(deviceName, remoteFolder, localPath string) error {
	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigLocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if cfg.SyncMappings == nil {
		cfg.SyncMappings = make(map[string]map[string]string)
	}
	if cfg.SyncMappings[deviceName] == nil {
		cfg.SyncMappings[deviceName] = make(map[string]string)
	}

	cfg.SyncMappings[deviceName][remoteFolder] = localPath
	return saveConfigLocked(cfg)
}

// SetSyncMappingsBatch atomically updates all folder mappings for a device.
func SetSyncMappingsBatch(deviceName string, mappings map[string]string) error {
	configMu.Lock()
	defer configMu.Unlock()

	cfg, err := loadConfigLocked()
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if cfg.SyncMappings == nil {
		cfg.SyncMappings = make(map[string]map[string]string)
	}
	if cfg.SyncMappings[deviceName] == nil {
		cfg.SyncMappings[deviceName] = make(map[string]string)
	}

	for k, v := range mappings {
		cfg.SyncMappings[deviceName][k] = v
	}
	return saveConfigLocked(cfg)
}

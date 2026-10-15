package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// HashToken returns the hex-encoded SHA-256 hash of a bearer token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

type SharedFolder struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	IsSync bool   `json:"is_sync,omitempty"`
}

type TrustedDevice struct {
	TokenHash  string    `json:"token_hash,omitempty"`
	Token      string    `json:"token,omitempty"` // legacy compatibility
	DeviceName string    `json:"device_name"`
	CreatedAt  time.Time `json:"created_at"`
	LastUsed   time.Time `json:"last_used"`
}

type Config struct {
	ServerName     string                   `json:"server_name"`
	Port           int                      `json:"port"`
	Folders        []SharedFolder           `json:"folders"`
	TrustedDevices map[string]TrustedDevice `json:"trusted_devices"` // key: tokenHash
}

type Manager struct {
	mu         sync.RWMutex
	configPath string
	lastLoaded time.Time
	config     Config
}

func NewManager(configDir string) (*Manager, error) {
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create config dir: %w", err)
	}

	configPath := filepath.Join(configDir, "qsync_config.json")
	mgr := &Manager{
		configPath: configPath,
		config: Config{
			ServerName:     "qSync Android",
			Port:           8484,
			Folders:        make([]SharedFolder, 0),
			TrustedDevices: make(map[string]TrustedDevice),
		},
	}
	mgr.mu.Lock()
	defer mgr.mu.Unlock()

	if err := mgr.loadLocked(); err != nil {
		// If file doesn't exist yet, save default
		if os.IsNotExist(err) {
			_ = mgr.saveLocked()
		} else {
			return nil, err
		}
	}

	return mgr, nil
}

func (m *Manager) ConfigDir() string {
	return filepath.Dir(m.configPath)
}

func (m *Manager) loadLocked() error {
	fi, err := os.Stat(m.configPath)
	if err != nil {
		return err
	}

	data, err := os.ReadFile(m.configPath)
	if err != nil {
		return err
	}

	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return err
	}

	if cfg.TrustedDevices == nil {
		cfg.TrustedDevices = make(map[string]TrustedDevice)
	} else {
		// Migrate any plaintext tokens to SHA-256 hashes
		migrated := make(map[string]TrustedDevice)
		needSave := false
		for k, v := range cfg.TrustedDevices {
			if v.TokenHash == "" {
				raw := k
				if v.Token != "" {
					raw = v.Token
				}
				h := HashToken(raw)
				v.TokenHash = h
				v.Token = "" // remove plaintext token from memory and disk
				migrated[h] = v
				needSave = true
			} else {
				migrated[k] = v
			}
		}
		cfg.TrustedDevices = migrated
		if needSave {
			m.config = cfg
			_ = m.saveLocked()
		}
	}
	if cfg.Folders == nil {
		cfg.Folders = make([]SharedFolder, 0)
	} else {
		for i := range cfg.Folders {
			cfg.Folders[i].Path = NormalizePath(cfg.Folders[i].Path)
		}
	}
	if cfg.Port <= 0 {
		cfg.Port = 8484
	}
	if cfg.ServerName == "" {
		cfg.ServerName = "qSync Android"
	}

	m.config = cfg
	m.lastLoaded = fi.ModTime()
	return nil
}

func (m *Manager) saveLocked() error {
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(m.configPath, data, 0600); err != nil {
		return err
	}
	if fi, err := os.Stat(m.configPath); err == nil {
		m.lastLoaded = fi.ModTime()
	}
	return nil
}

func (m *Manager) GetConfig() Config {
	m.mu.Lock()
	if fi, err := os.Stat(m.configPath); err == nil && fi.ModTime().After(m.lastLoaded) {
		_ = m.loadLocked()
	}
	m.mu.Unlock()

	m.mu.RLock()
	defer m.mu.RUnlock()
	cfg := m.config
	cfg.Folders = make([]SharedFolder, len(m.config.Folders))
	copy(cfg.Folders, m.config.Folders)
	cfg.TrustedDevices = make(map[string]TrustedDevice)
	for k, v := range m.config.TrustedDevices {
		cfg.TrustedDevices[k] = v
	}
	return cfg
}

func (m *Manager) AddFolder(name, folderPath string) (SharedFolder, error) {
	return m.AddFolderSync(name, folderPath, false)
}

func (m *Manager) AddFolderSync(name, folderPath string, isSync bool) (SharedFolder, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	normalized := NormalizePath(folderPath)
	if normalized == "" {
		return SharedFolder{}, fmt.Errorf("invalid empty folder path")
	}

	for _, f := range m.config.Folders {
		if f.Path == normalized || f.Name == name {
			return f, fmt.Errorf("folder with name '%s' or path '%s' already exists", name, normalized)
		}
	}

	id := fmt.Sprintf("f_%d", time.Now().UnixNano())
	folder := SharedFolder{
		ID:     id,
		Name:   name,
		Path:   normalized,
		IsSync: isSync,
	}
	m.config.Folders = append(m.config.Folders, folder)
	return folder, m.saveLocked()
}

func (m *Manager) RemoveFolder(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	idx := -1
	for i, f := range m.config.Folders {
		if f.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("folder not found")
	}

	m.config.Folders = append(m.config.Folders[:idx], m.config.Folders[idx+1:]...)
	return m.saveLocked()
}

func (m *Manager) AddTrustedDevice(token, deviceName string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now()
	h := HashToken(token)
	m.config.TrustedDevices[h] = TrustedDevice{
		TokenHash:  h,
		DeviceName: deviceName,
		CreatedAt:  now,
		LastUsed:   now,
	}
	return m.saveLocked()
}

func (m *Manager) UpdateDeviceLastUsed(tokenOrHash string) {
	m.mu.Lock()
	defer m.mu.Unlock()

	key := tokenOrHash
	if dev, ok := m.config.TrustedDevices[key]; ok {
		dev.LastUsed = time.Now()
		m.config.TrustedDevices[key] = dev
		_ = m.saveLocked()
		return
	}

	h := HashToken(tokenOrHash)
	if dev, ok := m.config.TrustedDevices[h]; ok {
		dev.LastUsed = time.Now()
		m.config.TrustedDevices[h] = dev
		_ = m.saveLocked()
	}
}

func (m *Manager) RevokeDevice(tokenOrHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	delete(m.config.TrustedDevices, tokenOrHash)
	delete(m.config.TrustedDevices, HashToken(tokenOrHash))
	return m.saveLocked()
}

func (m *Manager) SetServerSettings(name string, port int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if name != "" {
		m.config.ServerName = name
	}
	if port > 0 {
		m.config.Port = port
	}
	return m.saveLocked()
}

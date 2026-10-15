package keyring

import (
	"testing"
)

func TestUpdatePairedDevice(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// 1. Initial device pairing
	err := UpdatePairedDevice("Phone A", "192.168.1.10", 8484, "token-123")
	if err != nil {
		t.Fatalf("UpdatePairedDevice failed: %v", err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.PairedDevices) != 1 {
		t.Fatalf("expected 1 paired device, got %d", len(cfg.PairedDevices))
	}
	if dev, ok := cfg.PairedDevices["Phone A"]; !ok || dev.Token != "token-123" {
		t.Fatalf("unexpected device info: %+v", dev)
	}
	if cfg.LastServerName != "Phone A" {
		t.Fatalf("expected LastServerName 'Phone A', got %s", cfg.LastServerName)
	}

	// 2. Rename device (phone settings changed name from Phone A to Phone B with same IP and token)
	err = UpdatePairedDevice("Phone B", "192.168.1.10", 8484, "token-123")
	if err != nil {
		t.Fatalf("UpdatePairedDevice rename failed: %v", err)
	}

	cfg, err = LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig failed: %v", err)
	}
	if len(cfg.PairedDevices) != 1 {
		t.Fatalf("expected 1 paired device after rename, got %d", len(cfg.PairedDevices))
	}
	if _, ok := cfg.PairedDevices["Phone A"]; ok {
		t.Fatalf("old device name 'Phone A' still present in PairedDevices")
	}
	if dev, ok := cfg.PairedDevices["Phone B"]; !ok || dev.Token != "token-123" {
		t.Fatalf("unexpected renamed device info: %+v", dev)
	}
	if cfg.LastServerName != "Phone B" {
		t.Fatalf("expected LastServerName updated to 'Phone B', got %s", cfg.LastServerName)
	}
	if _, ok := cfg.Tokens[NormalizeServerKey("Phone A")]; ok {
		t.Fatalf("old token key for Phone A still present")
	}
	if token, ok := cfg.Tokens[NormalizeServerKey("Phone B")]; !ok || token != "token-123" {
		t.Fatalf("expected token under Phone B key, got %s", token)
	}
}

func TestSyncMappings(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// Test EnsureSyncFoldersInConfig
	folders := []string{"Audiobooks", "Notes"}
	err := EnsureSyncFoldersInConfig("Phone A", folders)
	if err != nil {
		t.Fatalf("EnsureSyncFoldersInConfig failed: %v", err)
	}

	mappings := GetSyncMappings("Phone A")
	if len(mappings) != 2 {
		t.Fatalf("expected 2 mappings, got %d", len(mappings))
	}
	if path, ok := mappings["Audiobooks"]; !ok || path != "" {
		t.Fatalf("expected empty path for Audiobooks, got '%s'", path)
	}

	// Test SetSyncMapping
	err = SetSyncMapping("Phone A", "Audiobooks", "/home/user/Audiobooks")
	if err != nil {
		t.Fatalf("SetSyncMapping failed: %v", err)
	}

	mappings = GetSyncMappings("Phone A")
	if mappings["Audiobooks"] != "/home/user/Audiobooks" {
		t.Fatalf("expected /home/user/Audiobooks, got %s", mappings["Audiobooks"])
	}

	// Test rename preservation
	err = UpdatePairedDevice("Phone A", "192.168.1.10", 8484, "token-123")
	if err != nil {
		t.Fatalf("initial pair failed: %v", err)
	}
	err = UpdatePairedDevice("Phone B", "192.168.1.10", 8484, "token-123")
	if err != nil {
		t.Fatalf("rename failed: %v", err)
	}

	bMappings := GetSyncMappings("Phone B")
	if bMappings["Audiobooks"] != "/home/user/Audiobooks" {
		t.Fatalf("expected mappings migrated to Phone B, got %v", bMappings)
	}
}

func TestTokenStorageAndIsolation(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// Save token for Phone1
	err := SaveToken("dev_phone1", "secret-token-phone1")
	if err != nil {
		t.Fatalf("SaveToken failed: %v", err)
	}

	// Save token for Phone2
	err = SaveToken("dev_phone2", "secret-token-phone2")
	if err != nil {
		t.Fatalf("SaveToken failed: %v", err)
	}

	// Verify isolation
	tok1, err := GetToken("dev_phone1")
	if err != nil || tok1 != "secret-token-phone1" {
		t.Fatalf("expected token-phone1, got: %s (err: %v)", tok1, err)
	}

	tok2, err := GetToken("dev_phone2")
	if err != nil || tok2 != "secret-token-phone2" {
		t.Fatalf("expected token-phone2, got: %s (err: %v)", tok2, err)
	}

	// Querying non-existent server MUST NOT return phone1 or phone2's token
	_, err = GetToken("dev_unknown_phone")
	if err == nil {
		t.Fatalf("expected error for unknown phone token, but got success")
	}

	// Delete token
	_ = DeleteToken("dev_phone1")
	_, err = GetToken("dev_phone1")
	if err == nil {
		t.Fatalf("token for phone1 should have been deleted")
	}
}

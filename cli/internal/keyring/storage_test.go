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

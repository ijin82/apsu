package auth

import (
	"path/filepath"
	"strings"
	"testing"

	"qsync/core/internal/storage"
)

func setupTestStore(t *testing.T) *storage.Manager {
	t.Helper()
	tempDir := t.TempDir()
	store, err := storage.NewManager(filepath.Join(tempDir, "config"))
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	return store
}

func TestPairingManager_Success(t *testing.T) {
	store := setupTestStore(t)
	mgr := NewPairingManager(store)

	pin, duration := mgr.GeneratePIN()
	if len(pin) != 6 {
		t.Fatalf("expected 6-digit PIN, got %s", pin)
	}
	if duration <= 0 {
		t.Fatalf("expected positive duration, got %v", duration)
	}

	activePIN, rem := mgr.GetActivePIN()
	if activePIN != pin || rem <= 0 {
		t.Fatalf("expected active PIN %s, got %s", pin, activePIN)
	}

	token, err := mgr.ValidatePINAndPair(pin, "Test-Desktop")
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if len(token) != 64 { // 32 bytes hex encoded = 64 chars
		t.Fatalf("expected 64 char hex token, got length %d", len(token))
	}

	// PIN should be invalidated after use
	if active, _ := mgr.GetActivePIN(); active != "" {
		t.Fatalf("expected PIN to be cleared after pairing, got %s", active)
	}

	// Token should be recognized
	if !mgr.IsTokenValid(token) {
		t.Fatalf("token should be valid")
	}
	if mgr.IsTokenValid("non-existent-token") {
		t.Fatalf("unknown token should be invalid")
	}
}

func TestPairingManager_BruteForceLockout(t *testing.T) {
	store := setupTestStore(t)
	mgr := NewPairingManager(store)

	pin, _ := mgr.GeneratePIN()

	// 4 wrong attempts should warn with remaining attempts
	for i := 1; i < MaxFailedAttempts; i++ {
		_, err := mgr.ValidatePINAndPair("000000", "Attacker")
		if err == nil {
			t.Fatalf("attempt %d should fail", i)
		}
		expectedRem := MaxFailedAttempts - i
		if !strings.Contains(err.Error(), "attempts remaining") {
			t.Fatalf("attempt %d error should mention remaining attempts, got: %v", i, err)
		}
		_ = expectedRem
	}

	// 5th wrong attempt triggers lockout
	_, err := mgr.ValidatePINAndPair("000000", "Attacker")
	if err == nil {
		t.Fatalf("5th attempt should fail")
	}
	if !strings.Contains(err.Error(), "too many invalid PIN attempts") {
		t.Fatalf("expected lockout message, got: %v", err)
	}

	// PIN should now be cleared
	if active, _ := mgr.GetActivePIN(); active != "" {
		t.Fatalf("active PIN should be cleared upon lockout, got %s", active)
	}

	// Even with the original PIN, pairing must be rejected during lockout
	_, err = mgr.ValidatePINAndPair(pin, "Attacker")
	if err == nil || !strings.Contains(err.Error(), "pairing locked") {
		t.Fatalf("pairing should be locked, got: %v", err)
	}

	// Generating PIN during lockout should also be refused
	newPin, remLockout := mgr.GeneratePIN()
	if newPin != "" || remLockout <= 0 {
		t.Fatalf("GeneratePIN during lockout should return empty pin, got pin=%s, rem=%v", newPin, remLockout)
	}
}

func TestPairingManager_ExpiredOrEmptyPIN(t *testing.T) {
	store := setupTestStore(t)
	mgr := NewPairingManager(store)

	_, err := mgr.ValidatePINAndPair("123456", "PC")
	if err == nil || !strings.Contains(err.Error(), "no active pairing session") {
		t.Fatalf("expected no active pairing session error, got: %v", err)
	}

	pin, _ := mgr.GeneratePIN()
	mgr.ClearPIN()

	_, err = mgr.ValidatePINAndPair(pin, "PC")
	if err == nil || !strings.Contains(err.Error(), "no active pairing session") {
		t.Fatalf("expected no active pairing session after ClearPIN, got: %v", err)
	}
}

func TestTokenHashingAndStorage(t *testing.T) {
	store := setupTestStore(t)
	mgr := NewPairingManager(store)

	pin, _ := mgr.GeneratePIN()
	token, err := mgr.ValidatePINAndPair(pin, "Secure-Laptop")
	if err != nil {
		t.Fatalf("pairing failed: %v", err)
	}

	cfg := store.GetConfig()
	// Raw token MUST NOT exist as a key in cfg.TrustedDevices
	if _, exists := cfg.TrustedDevices[token]; exists {
		t.Fatalf("security violation: raw token stored in config!")
	}

	// Token hash MUST exist
	expectedHash := storage.HashToken(token)
	dev, exists := cfg.TrustedDevices[expectedHash]
	if !exists {
		t.Fatalf("expected token hash %s to exist in config", expectedHash)
	}
	if dev.Token != "" {
		t.Fatalf("security violation: dev.Token contains plaintext token!")
	}
	if dev.TokenHash != expectedHash {
		t.Fatalf("expected TokenHash %s, got %s", expectedHash, dev.TokenHash)
	}

	// Revoke by raw token (which gets hashed) or by hash
	if err := store.RevokeDevice(token); err != nil {
		t.Fatalf("RevokeDevice failed: %v", err)
	}
	if mgr.IsTokenValid(token) {
		t.Fatalf("revoked token should no longer be valid")
	}
}

func TestDeviceNameSanitization(t *testing.T) {
	store := setupTestStore(t)
	mgr := NewPairingManager(store)

	// 1. Control chars & whitespace
	pin, _ := mgr.GeneratePIN()
	dirtyName := "  My\x00Desktop\tPC\r\n  "
	token, err := mgr.ValidatePINAndPair(pin, dirtyName)
	if err != nil {
		t.Fatalf("pairing failed: %v", err)
	}
	h := storage.HashToken(token)
	dev := store.GetConfig().TrustedDevices[h]
	if dev.DeviceName != "MyDesktopPC" {
		t.Fatalf("expected 'MyDesktopPC', got %q", dev.DeviceName)
	}

	// 2. Empty or only spaces/controls
	pin, _ = mgr.GeneratePIN()
	emptyName := "   \x00\r\n   "
	token, err = mgr.ValidatePINAndPair(pin, emptyName)
	if err != nil {
		t.Fatalf("pairing failed: %v", err)
	}
	h = storage.HashToken(token)
	dev = store.GetConfig().TrustedDevices[h]
	if dev.DeviceName != "Unknown PC" {
		t.Fatalf("expected 'Unknown PC', got %q", dev.DeviceName)
	}

	// 3. Truncation to 64 runes
	pin, _ = mgr.GeneratePIN()
	longName := strings.Repeat("A", 100)
	token, err = mgr.ValidatePINAndPair(pin, longName)
	if err != nil {
		t.Fatalf("pairing failed: %v", err)
	}
	h = storage.HashToken(token)
	dev = store.GetConfig().TrustedDevices[h]
	if len([]rune(dev.DeviceName)) != 64 {
		t.Fatalf("expected 64 runes, got %d", len([]rune(dev.DeviceName)))
	}
}

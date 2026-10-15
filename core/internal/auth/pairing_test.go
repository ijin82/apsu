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

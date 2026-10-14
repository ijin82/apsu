package auth

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"math/big"
	"sync"
	"time"

	"qsync/core/internal/storage"
)

type PairingManager struct {
	mu           sync.Mutex
	store        *storage.Manager
	activePIN    string
	pinExpiresAt time.Time
}

func NewPairingManager(store *storage.Manager) *PairingManager {
	return &PairingManager{
		store: store,
	}
}

// GeneratePIN creates a new 6-digit PIN valid for 5 minutes
func (p *PairingManager) GeneratePIN() (string, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Generate 6 digit number
	n, _ := rand.Int(rand.Reader, big.NewInt(900000))
	p.activePIN = fmt.Sprintf("%06d", n.Int64()+100000)
	duration := 5 * time.Minute
	p.pinExpiresAt = time.Now().Add(duration)

	return p.activePIN, duration
}

// ClearPIN invalidates the active PIN
func (p *PairingManager) ClearPIN() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.activePIN = ""
	p.pinExpiresAt = time.Time{}
}

// GetActivePIN returns current PIN if valid, or empty string
func (p *PairingManager) GetActivePIN() (string, time.Duration) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.activePIN == "" || time.Now().After(p.pinExpiresAt) {
		p.activePIN = ""
		return "", 0
	}
	return p.activePIN, time.Until(p.pinExpiresAt)
}

// ValidatePINAndPair checks the PIN and issues a new auth token for the device
func (p *PairingManager) ValidatePINAndPair(pin, deviceName string) (string, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.activePIN == "" || time.Now().After(p.pinExpiresAt) {
		return "", fmt.Errorf("no active pairing session or PIN expired")
	}

	if p.activePIN != pin {
		return "", fmt.Errorf("invalid PIN")
	}

	// Invalidate PIN after successful use
	p.activePIN = ""
	p.pinExpiresAt = time.Time{}

	// Generate 32-byte crypto token
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate secure token: %w", err)
	}
	token := hex.EncodeToString(b)

	if deviceName == "" {
		deviceName = "Unknown PC"
	}

	if err := p.store.AddTrustedDevice(token, deviceName); err != nil {
		return "", fmt.Errorf("failed to store trusted device: %w", err)
	}

	return token, nil
}

// IsTokenValid checks whether the given token is recognized and updates its last_used timestamp
func (p *PairingManager) IsTokenValid(token string) bool {
	if token == "" {
		return false
	}
	cfg := p.store.GetConfig()
	if _, exists := cfg.TrustedDevices[token]; exists {
		go p.store.UpdateDeviceLastUsed(token)
		return true
	}
	return false
}

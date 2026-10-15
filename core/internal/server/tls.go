package server

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"strings"
	"time"
)

// FingerprintBytes returns uppercase hex SHA-256 fingerprint of a DER certificate
func FingerprintBytes(der []byte) string {
	sum := sha256.Sum256(der)
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// GetCertificateFingerprintFromFile reads a PEM cert file and returns its SHA-256 fingerprint
func GetCertificateFingerprintFromFile(certPath string) (string, error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return "", err
	}
	block, _ := pem.Decode(data)
	if block == nil || block.Type != "CERTIFICATE" {
		return "", fmt.Errorf("failed to decode PEM certificate from %s", certPath)
	}
	return FingerprintBytes(block.Bytes), nil
}

// EnsureTLSCertificate ensures that a self-signed TLS cert and private key exist.
// If not found, it generates a new ECDSA P-256 certificate valid for 10 years.
// It returns the uppercase SHA-256 fingerprint of the certificate.
func EnsureTLSCertificate(certPath, keyPath string, hostnames []string, ips []string) (string, error) {
	if certData, errCert := os.ReadFile(certPath); errCert == nil {
		if _, errKey := os.Stat(keyPath); errKey == nil {
			block, _ := pem.Decode(certData)
			if block != nil && block.Type == "CERTIFICATE" {
				if parsedCert, err := x509.ParseCertificate(block.Bytes); err == nil && time.Now().Before(parsedCert.NotAfter) {
					// Verify that all requested ips are already in SAN
					allCovered := true
					for _, ipStr := range ips {
						targetIP := net.ParseIP(ipStr)
						if targetIP == nil {
							continue
						}
						found := false
						for _, certIP := range parsedCert.IPAddresses {
							if certIP.Equal(targetIP) {
								found = true
								break
							}
						}
						if !found {
							allCovered = false
							break
						}
					}
					if allCovered {
						return FingerprintBytes(block.Bytes), nil
					}
				}
			}
		}
	}

	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to generate private key: %w", err)
	}

	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serialNumber, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return "", fmt.Errorf("failed to generate serial number: %w", err)
	}

	template := x509.Certificate{
		SerialNumber: serialNumber,
		Subject: pkix.Name{
			Organization: []string{"qSync"},
			CommonName:   "qSync Secure Server",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour), // 10 years
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	// Add localhost IP and names
	template.IPAddresses = append(template.IPAddresses, net.ParseIP("127.0.0.1"), net.IPv6loopback)
	template.DNSNames = append(template.DNSNames, "localhost", "qsync.local")

	for _, h := range hostnames {
		if h != "" && h != "localhost" {
			template.DNSNames = append(template.DNSNames, h)
		}
	}
	for _, ipStr := range ips {
		if ip := net.ParseIP(ipStr); ip != nil {
			template.IPAddresses = append(template.IPAddresses, ip)
		}
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return "", fmt.Errorf("failed to create certificate: %w", err)
	}

	// Write cert PEM
	certOut, err := os.OpenFile(certPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0644)
	if err != nil {
		return "", fmt.Errorf("failed to open cert file for writing: %w", err)
	}
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
		certOut.Close()
		return "", fmt.Errorf("failed to encode cert PEM: %w", err)
	}
	certOut.Close()

	// Write key PEM
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		return "", fmt.Errorf("failed to marshal private key: %w", err)
	}
	keyOut, err := os.OpenFile(keyPath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return "", fmt.Errorf("failed to open key file for writing: %w", err)
	}
	if err := pem.Encode(keyOut, &pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes}); err != nil {
		keyOut.Close()
		return "", fmt.Errorf("failed to encode key PEM: %w", err)
	}
	keyOut.Close()

	return FingerprintBytes(derBytes), nil
}

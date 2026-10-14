package client

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"
)

type PairRequest struct {
	PIN        string `json:"pin"`
	DeviceName string `json:"device_name"`
}

type PairResponse struct {
	Success    bool   `json:"success"`
	Token      string `json:"token,omitempty"`
	ServerName string `json:"server_name,omitempty"`
	Error      string `json:"error,omitempty"`
}

func Pair(ip string, port int, pin string, deviceName string) (string, string, error) {
	if deviceName == "" {
		host, err := os.Hostname()
		if err == nil && host != "" {
			deviceName = host
		} else {
			deviceName = "Linux PC"
		}
	}

	reqBody, err := json.Marshal(PairRequest{
		PIN:        pin,
		DeviceName: deviceName,
	})
	if err != nil {
		return "", "", err
	}

	url := fmt.Sprintf("http://%s:%d/api/pair", ip, port)
	httpClient := &http.Client{Timeout: 5 * time.Second}

	resp, err := httpClient.Post(url, "application/json", bytes.NewReader(reqBody))
	if err != nil {
		return "", "", fmt.Errorf("Connection to %s failed: %w", url, err)
	}
	defer resp.Body.Close()

	var pairResp PairResponse
	if err := json.NewDecoder(resp.Body).Decode(&pairResp); err != nil {
		return "", "", fmt.Errorf("Failed to decode response: %w", err)
	}

	if !pairResp.Success {
		return "", "", fmt.Errorf("Pairing failed: %s", pairResp.Error)
	}

	return pairResp.Token, pairResp.ServerName, nil
}

// Ping checks if the server is accessible
func Ping(ip string, port int) bool {
	httpClient := &http.Client{Timeout: 2 * time.Second}
	url := fmt.Sprintf("http://%s:%d/", ip, port)
	resp, err := httpClient.Get(url)
	if err != nil {
		return false
	}
	resp.Body.Close()
	// Server returns 401 Unauthorized for unauthenticated requests, which means it is alive!
	return resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusOK
}

type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Port    int    `json:"port"`
}

// FetchServerName queries /api/info to retrieve the actual device name set in phone settings
func FetchServerName(ip string, port int) string {
	httpClient := &http.Client{Timeout: 800 * time.Millisecond}
	url := fmt.Sprintf("http://%s:%d/api/info", ip, port)
	resp, err := httpClient.Get(url)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		var info ServerInfo
		if err := json.NewDecoder(resp.Body).Decode(&info); err == nil && info.Name != "" {
			return info.Name
		}
	}
	return ""
}

// VerifyToken checks whether the provided token is accepted by the phone server.
// It first attempts the lightweight /api/auth/verify endpoint, and falls back to
// WebDAV root with Authorization header for backward compatibility.
func VerifyToken(ip string, port int, token string) bool {
	if token == "" || ip == "" {
		return false
	}
	if port <= 0 {
		port = 8484
	}

	httpClient := &http.Client{Timeout: 1200 * time.Millisecond}

	// 1. Try /api/auth/verify
	verifyURL := fmt.Sprintf("http://%s:%d/api/auth/verify", ip, port)
	req, err := http.NewRequest(http.MethodGet, verifyURL, nil)
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+token)
		resp, err := httpClient.Do(req)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return true
			}
			if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
				return false
			}
			// If not 404, but something else, still fall back to DAV check
		}
	}

	// 2. Fallback: WebDAV check on root
	davURL := fmt.Sprintf("http://%s:%d/", ip, port)
	reqDAV, err := http.NewRequest("PROPFIND", davURL, nil)
	if err == nil {
		reqDAV.Header.Set("Authorization", "Bearer "+token)
		reqDAV.Header.Set("Depth", "0")
		resp, err := httpClient.Do(reqDAV)
		if err == nil {
			resp.Body.Close()
			return resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden
		}
	}

	// 3. Fallback: GET on root with Bearer token
	reqGET, err := http.NewRequest(http.MethodGet, davURL, nil)
	if err == nil {
		reqGET.Header.Set("Authorization", "Bearer "+token)
		resp, err := httpClient.Do(reqGET)
		if err == nil {
			resp.Body.Close()
			return resp.StatusCode != http.StatusUnauthorized && resp.StatusCode != http.StatusForbidden
		}
	}

	return false
}

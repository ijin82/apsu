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

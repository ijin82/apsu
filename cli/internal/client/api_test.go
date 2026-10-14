package client

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

func TestVerifyToken(t *testing.T) {
	// 1. Mock server with /api/auth/verify endpoint
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/verify" {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "Bearer valid-token" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{"valid":true}`))
			} else {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"valid":false}`))
			}
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	parts := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")
	ip := parts[0]
	port, _ := strconv.Atoi(parts[1])

	if !VerifyToken(ip, port, "valid-token") {
		t.Fatalf("expected VerifyToken to return true for valid-token")
	}
	if VerifyToken(ip, port, "wrong-token") {
		t.Fatalf("expected VerifyToken to return false for wrong-token")
	}
	if VerifyToken(ip, port, "") {
		t.Fatalf("expected VerifyToken to return false for empty token")
	}
}

func TestVerifyTokenFallback(t *testing.T) {
	// 2. Mock older server without /api/auth/verify (returns 404), fallback to WebDAV root
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/auth/verify" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if r.URL.Path == "/" {
			authHeader := r.Header.Get("Authorization")
			if authHeader == "Bearer old-valid-token" {
				w.WriteHeader(http.StatusMultiStatus)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	parts := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")
	ip := parts[0]
	port, _ := strconv.Atoi(parts[1])

	if !VerifyToken(ip, port, "old-valid-token") {
		t.Fatalf("expected VerifyToken fallback to return true for old-valid-token")
	}
	if VerifyToken(ip, port, "bad-token") {
		t.Fatalf("expected VerifyToken fallback to return false for bad-token")
	}
}

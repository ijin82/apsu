package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/webdav"
	"qsync/core/internal/auth"
	"qsync/core/internal/storage"
)

type Server struct {
	mu         sync.RWMutex
	store      *storage.Manager
	pairingMgr *auth.PairingManager
	httpServer *http.Server
	listener   net.Listener
	running    bool
	boundPort  int
}

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

func NewServer(store *storage.Manager, pairingMgr *auth.PairingManager) *Server {
	return &Server{
		store:      store,
		pairingMgr: pairingMgr,
	}
}

func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.running {
		return fmt.Errorf("server already running")
	}

	cfg := s.store.GetConfig()
	addr := fmt.Sprintf("0.0.0.0:%d", cfg.Port)
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", addr, err)
	}

	s.listener = ln
	s.boundPort = ln.Addr().(*net.TCPAddr).Port

	vfs := NewVirtualFS(s.store)
	davHandler := &webdav.Handler{
		Prefix:     "",
		FileSystem: vfs,
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil {
				fmt.Printf("[WebDAV Error] %s %s: %v\n", r.Method, r.URL.Path, err)
			}
		},
	}

	mux := http.NewServeMux()

	// API endpoints
	mux.HandleFunc("/api/info", s.handleInfo)
	mux.HandleFunc("/api/auth/verify", s.handleAuthVerify)
	mux.HandleFunc("/api/debug", s.handleDebug)
	mux.HandleFunc("/api/pair", s.handlePair)
	mux.HandleFunc("/api/pin", s.handleGeneratePIN)
	mux.HandleFunc("/api/folders", s.handleFoldersAPI)

	// WebDAV with Auth check
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.handleWebDAV(davHandler, w, r)
	})

	s.httpServer = &http.Server{
		Handler:      mux,
		ReadTimeout:  1 * time.Hour,
		WriteTimeout: 1 * time.Hour,
	}

	s.running = true

	go func() {
		if err := s.httpServer.Serve(ln); err != nil && err != http.ErrServerClosed {
			s.mu.Lock()
			s.running = false
			s.mu.Unlock()
		}
	}()

	return nil
}

func (s *Server) Stop() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if !s.running || s.httpServer == nil {
		return nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err := s.httpServer.Shutdown(ctx)
	s.running = false
	s.httpServer = nil
	s.listener = nil
	return err
}

func (s *Server) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.running
}

func (s *Server) Port() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.boundPort
}

func (s *Server) handlePair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req PairRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(PairResponse{Success: false, Error: "invalid JSON"})
		return
	}

	token, err := s.pairingMgr.ValidatePINAndPair(req.PIN, req.DeviceName)
	w.Header().Set("Content-Type", "application/json")
	if err != nil {
		w.WriteHeader(http.StatusUnauthorized)
		_ = json.NewEncoder(w).Encode(PairResponse{Success: false, Error: err.Error()})
		return
	}

	cfg := s.store.GetConfig()
	_ = json.NewEncoder(w).Encode(PairResponse{
		Success:    true,
		Token:      token,
		ServerName: cfg.ServerName,
	})
}

func (s *Server) handleAuthVerify(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	token := ""
	if _, pass, ok := r.BasicAuth(); ok {
		token = pass
	} else if authHeader := r.Header.Get("Authorization"); strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
	}

	if token != "" && s.pairingMgr.IsTokenValid(token) {
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"valid":       true,
			"server_name": s.store.GetConfig().ServerName,
		})
		return
	}

	w.WriteHeader(http.StatusUnauthorized)
	_ = json.NewEncoder(w).Encode(map[string]interface{}{
		"valid": false,
		"error": "invalid or expired token",
	})
}

func (s *Server) handleWebDAV(handler *webdav.Handler, w http.ResponseWriter, r *http.Request) {
	// WebDAV protocol headers required by GVfs / Nautilus
	w.Header().Set("DAV", "1, 2")
	w.Header().Set("MS-Author-Via", "DAV")
	w.Header().Set("Allow", "OPTIONS, GET, HEAD, POST, PUT, DELETE, TRACE, PROPFIND, PROPPATCH, MKCOL, COPY, MOVE, LOCK, UNLOCK")

	if r.Method == http.MethodOptions {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 1. Check HTTP Basic Auth (password is token)
	if _, pass, ok := r.BasicAuth(); ok {
		if s.pairingMgr.IsTokenValid(pass) {
			handler.ServeHTTP(w, r)
			return
		}
	}

	// 2. Check Bearer token in Authorization header
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token := strings.TrimSpace(strings.TrimPrefix(authHeader, "Bearer "))
		if s.pairingMgr.IsTokenValid(token) {
			handler.ServeHTTP(w, r)
			return
		}
	}

	// Unauthorized
	w.Header().Set("WWW-Authenticate", `Basic realm="qSync"`)
	http.Error(w, "Unauthorized: Pairing required", http.StatusUnauthorized)
}

type InfoResponse struct {
	Name    string                 `json:"name"`
	Version string                 `json:"version"`
	Port    int                    `json:"port"`
	Folders []storage.SharedFolder `json:"folders"`
}

type FolderDebugInfo struct {
	Name         string   `json:"name"`
	Path         string   `json:"path"`
	Exists       bool     `json:"exists"`
	IsDir        bool     `json:"is_dir"`
	Mode         string   `json:"mode"`
	ReadDirCount int      `json:"readdir_count"`
	ReadDirErr   string   `json:"readdir_err,omitempty"`
	SampleFiles  []string `json:"sample_files"`
}

type DebugResponse struct {
	UID         int               `json:"uid"`
	GID         int               `json:"gid"`
	StorageRoot string            `json:"storage_root"`
	RootCount   int               `json:"root_count"`
	RootErr     string            `json:"root_err,omitempty"`
	RootDirs    []string          `json:"root_dirs"`
	Folders     []FolderDebugInfo `json:"folders"`
}

func (s *Server) handleInfo(w http.ResponseWriter, r *http.Request) {
	cfg := s.store.GetConfig()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(InfoResponse{
		Name:    cfg.ServerName,
		Version: "1.0",
		Port:    s.boundPort,
		Folders: cfg.Folders,
	})
}

func (s *Server) handleGeneratePIN(w http.ResponseWriter, r *http.Request) {
	pin, _ := s.pairingMgr.GeneratePIN()
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{"pin": pin})
}

func (s *Server) handleFoldersAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		cfg := s.store.GetConfig()
		_ = json.NewEncoder(w).Encode(cfg.Folders)
	case http.MethodPost:
		var req struct {
			Name   string `json:"name"`
			Path   string `json:"path"`
			IsSync bool   `json:"is_sync"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f, err := s.store.AddFolderSync(req.Name, req.Path, req.IsSync)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		_ = json.NewEncoder(w).Encode(f)
	case http.MethodDelete:
		id := r.URL.Query().Get("id")
		if id != "" {
			_ = s.store.RemoveFolder(id)
		}
		_ = json.NewEncoder(w).Encode(map[string]bool{"success": true})
	}
}

func (s *Server) handleDebug(w http.ResponseWriter, r *http.Request) {
	resp := DebugResponse{
		UID:         os.Getuid(),
		GID:         os.Getgid(),
		StorageRoot: "/storage/emulated/0",
		Folders:     make([]FolderDebugInfo, 0),
	}

	if entries, err := os.ReadDir("/storage/emulated/0"); err != nil {
		resp.RootErr = err.Error()
	} else {
		resp.RootCount = len(entries)
		for _, e := range entries {
			if e.IsDir() {
				resp.RootDirs = append(resp.RootDirs, e.Name())
			}
		}
	}

	cfg := s.store.GetConfig()
	for _, f := range cfg.Folders {
		info := FolderDebugInfo{
			Name: f.Name,
			Path: f.Path,
		}
		fi, err := os.Stat(f.Path)
		if err != nil {
			info.Exists = false
			info.ReadDirErr = "stat error: " + err.Error()
		} else {
			info.Exists = true
			info.IsDir = fi.IsDir()
			info.Mode = fi.Mode().String()

			entries, err := os.ReadDir(f.Path)
			if err != nil {
				info.ReadDirErr = "readdir error: " + err.Error()
			} else {
				info.ReadDirCount = len(entries)
				for i, e := range entries {
					if i >= 10 {
						break
					}
					t := "file"
					if e.IsDir() {
						t = "dir"
					}
					info.SampleFiles = append(info.SampleFiles, fmt.Sprintf("%s (%s)", e.Name(), t))
				}
			}
		}
		resp.Folders = append(resp.Folders, info)
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

// GetLocalIPs returns non-loopback IPv4 addresses, prioritizing Wi-Fi
func GetLocalIPs() []string {
	var wifiIPs []string
	var otherIPs []string

	ifaces, err := net.Interfaces()
	if err != nil {
		return wifiIPs
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		isWiFi := strings.HasPrefix(iface.Name, "wlan") || strings.HasPrefix(iface.Name, "wl")

		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip != nil && ip.To4() != nil && !ip.IsLoopback() {
				ipStr := ip.String()
				if isWiFi {
					wifiIPs = append(wifiIPs, ipStr)
				} else {
					otherIPs = append(otherIPs, ipStr)
				}
			}
		}
	}

	return append(wifiIPs, otherIPs...)
}

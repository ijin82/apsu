package sync

import (
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"apsu/cli/internal/keyring"
)

func TestSyncEngine(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	// Create local source folder with some files
	localSource := filepath.Join(tempDir, "LocalMusic")
	_ = os.MkdirAll(filepath.Join(localSource, "Subdir"), 0755)
	_ = os.WriteFile(filepath.Join(localSource, "file1.txt"), []byte("Hello 1"), 0644)
	_ = os.WriteFile(filepath.Join(localSource, "Subdir", "file2.txt"), []byte("Hello 2"), 0644)

	// In-memory remote storage for mock WebDAV server
	remoteFiles := map[string][]byte{
		"Music/old_to_delete.txt": []byte("Delete me"),
	}
	remoteDirs := map[string]bool{
		"Music": true,
	}
	var lock sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lock.Lock()
		defer lock.Unlock()

		cleanPath := strings.TrimPrefix(r.URL.Path, "/")

		switch r.Method {
		case "GET":
			if r.URL.Path == "/api/info" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"name": "TestPhone",
					"folders": [
						{"id": "1", "name": "Music", "path": "/storage/Music", "is_sync": true}
					]
				}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)

		case "PROPFIND":
			type ResType struct {
				Collection *struct{} `xml:"D:collection"`
			}
			type Prop struct {
				ResourceType     ResType `xml:"D:resourcetype"`
				GetContentLength int64   `xml:"D:getcontentlength"`
				GetLastModified  string  `xml:"D:getlastmodified"`
			}
			type PropStat struct {
				Prop   Prop   `xml:"D:prop"`
				Status string `xml:"D:status"`
			}
			type Resp struct {
				Href     string   `xml:"D:href"`
				PropStat PropStat `xml:"D:propstat"`
			}
			type MultiStatus struct {
				XMLName   xml.Name `xml:"D:multistatus"`
				Xmlns     string   `xml:"xmlns:D,attr"`
				Responses []Resp   `xml:"D:response"`
			}

			ms := MultiStatus{
				Xmlns: "DAV:",
				Responses: []Resp{
					{
						Href: "/Music/",
						PropStat: PropStat{
							Prop:   Prop{ResourceType: ResType{Collection: &struct{}{}}},
							Status: "HTTP/1.1 200 OK",
						},
					},
				},
			}

			for f, data := range remoteFiles {
				ms.Responses = append(ms.Responses, Resp{
					Href: "/" + f,
					PropStat: PropStat{
						Prop: Prop{
							GetContentLength: int64(len(data)),
							GetLastModified:  time.Now().Add(-1 * time.Hour).UTC().Format(http.TimeFormat),
						},
						Status: "HTTP/1.1 200 OK",
					},
				})
			}
			for d := range remoteDirs {
				if d != "Music" {
					ms.Responses = append(ms.Responses, Resp{
						Href: "/" + d + "/",
						PropStat: PropStat{
							Prop:   Prop{ResourceType: ResType{Collection: &struct{}{}}},
							Status: "HTTP/1.1 200 OK",
						},
					})
				}
			}

			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			_ = xml.NewEncoder(w).Encode(ms)

		case "MKCOL":
			remoteDirs[cleanPath] = true
			w.WriteHeader(http.StatusCreated)

		case "PUT":
			data, _ := io.ReadAll(r.Body)
			remoteFiles[cleanPath] = data
			w.WriteHeader(http.StatusCreated)

		case "DELETE":
			delete(remoteFiles, cleanPath)
			delete(remoteDirs, cleanPath)
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	parts := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")
	ip := parts[0]
	port, _ := strconv.Atoi(parts[1])

	// Configure mapping
	_ = keyring.SetSyncMapping("TestPhone", "Music", localSource)

	logPath := filepath.Join(tempDir, "sync_history.log")
	logger, _ := NewSyncLogger(logPath)
	engine := NewSyncEngine(logger)

	stats, summary, err := engine.SyncDevice("TestPhone", ip, port, "token-123")
	if err != nil {
		t.Fatalf("SyncDevice failed: %v", err)
	}

	if stats.CreatedCount != 2 {
		t.Fatalf("expected 2 created files (file1.txt, Subdir/file2.txt), got %d", stats.CreatedCount)
	}
	if stats.DeletedCount != 1 {
		t.Fatalf("expected 1 deleted file (old_to_delete.txt), got %d", stats.DeletedCount)
	}

	lock.Lock()
	if _, exists := remoteFiles["Music/old_to_delete.txt"]; exists {
		t.Fatalf("old_to_delete.txt should have been deleted from remote")
	}
	if string(remoteFiles["Music/file1.txt"]) != "Hello 1" {
		t.Fatalf("file1.txt was not uploaded correctly")
	}
	if string(remoteFiles["Music/Subdir/file2.txt"]) != "Hello 2" {
		t.Fatalf("Subdir/file2.txt was not uploaded correctly")
	}
	lock.Unlock()

	// Verify log contains session
	logContent, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}
	if !strings.Contains(string(logContent), "CREATE FILE \"file1.txt\"") {
		t.Fatalf("log missing CREATE FILE file1.txt")
	}
	if !strings.Contains(string(logContent), "DELETE FILE \"old_to_delete.txt\"") {
		t.Fatalf("log missing DELETE FILE old_to_delete.txt")
	}
	fmt.Printf("Sync completed successfully: %s\n", summary)
}

func TestIsProtectedPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		home = "/home/testuser"
	}

	tests := []struct {
		path      string
		protected bool
	}{
		{"/", true},
		{"/etc", true},
		{"/usr", true},
		{"/var", true},
		{"/tmp", true},
		{home, true},
		{filepath.Join(home, ".ssh"), true},
		{filepath.Join(home, ".ssh", "id_rsa"), true},
		{filepath.Join(home, ".gnupg"), true},
		{filepath.Join(home, ".config"), true},
		{filepath.Join(home, ".config", "apsu"), true},
		{filepath.Join(home, "Music"), false},
		{filepath.Join(home, "Documents", "Apsu"), false},
		{filepath.Join(home, "Downloads"), false},
	}

	for _, tt := range tests {
		got, reason := IsProtectedPath(tt.path)
		if got != tt.protected {
			t.Errorf("IsProtectedPath(%q) = %v (reason: %q), expected %v", tt.path, got, reason, tt.protected)
		}
	}
}

func TestSyncEngine_SafetyChecks(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	emptyLocal := filepath.Join(tempDir, "EmptyLocal")
	_ = os.MkdirAll(emptyLocal, 0755)

	remoteFiles := map[string][]byte{
		"Backup/photo1.jpg": []byte("image data 1"),
		"Backup/photo2.jpg": []byte("image data 2"),
		"Backup/photo3.jpg": []byte("image data 3"),
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			if r.URL.Path == "/api/info" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"name": "SafetyPhone",
					"folders": [
						{"id": "1", "name": "Backup", "path": "/storage/Backup", "is_sync": true}
					]
				}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case "PROPFIND":
			type ResType struct {
				Collection *struct{} `xml:"D:collection"`
			}
			type Prop struct {
				ResourceType     ResType `xml:"D:resourcetype"`
				GetContentLength int64   `xml:"D:getcontentlength"`
				GetLastModified  string  `xml:"D:getlastmodified"`
			}
			type PropStat struct {
				Prop   Prop   `xml:"D:prop"`
				Status string `xml:"D:status"`
			}
			type Resp struct {
				Href     string   `xml:"D:href"`
				PropStat PropStat `xml:"D:propstat"`
			}
			type MultiStatus struct {
				XMLName   xml.Name `xml:"D:multistatus"`
				Xmlns     string   `xml:"xmlns:D,attr"`
				Responses []Resp   `xml:"D:response"`
			}

			ms := MultiStatus{
				Xmlns: "DAV:",
				Responses: []Resp{
					{
						Href: "/Backup/",
						PropStat: PropStat{
							Prop:   Prop{ResourceType: ResType{Collection: &struct{}{}}},
							Status: "HTTP/1.1 200 OK",
						},
					},
				},
			}
			for f, data := range remoteFiles {
				ms.Responses = append(ms.Responses, Resp{
					Href: "/" + f,
					PropStat: PropStat{
						Prop: Prop{
							GetContentLength: int64(len(data)),
							GetLastModified:  time.Now().Add(-1 * time.Hour).UTC().Format(http.TimeFormat),
						},
						Status: "HTTP/1.1 200 OK",
					},
				})
			}
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			_ = xml.NewEncoder(w).Encode(ms)
		case "DELETE":
			delete(remoteFiles, strings.TrimPrefix(r.URL.Path, "/"))
			w.WriteHeader(http.StatusNoContent)
		}
	}))
	defer server.Close()

	parts := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")
	ip := parts[0]
	port, _ := strconv.Atoi(parts[1])

	_ = keyring.SetSyncMapping("SafetyPhone", "Backup", emptyLocal)

	engine := NewSyncEngine(nil)

	// 1. Safe mode (AllowMassDeletion = false): MUST abort deletion of remote files when local is empty
	stats, _, err := engine.SyncDevice("SafetyPhone", ip, port, "token-123")
	if stats.DeletedCount != 0 {
		t.Fatalf("expected 0 deleted files in safe mode, got %d", stats.DeletedCount)
	}
	if len(remoteFiles) != 3 {
		t.Fatalf("remote files were wiped! Expected 3, got %d", len(remoteFiles))
	}
	_ = err

	// 2. AllowMassDeletion = true: MUST allow deletion
	engine.AllowMassDeletion = true
	stats, _, err = engine.SyncDevice("SafetyPhone", ip, port, "token-123")
	if err != nil {
		t.Fatalf("SyncDevice with AllowMassDeletion failed: %v", err)
	}
	if stats.DeletedCount != 3 {
		t.Fatalf("expected 3 deleted files with AllowMassDeletion, got %d", stats.DeletedCount)
	}
	if len(remoteFiles) != 0 {
		t.Fatalf("expected remote files to be cleared with AllowMassDeletion, got %d", len(remoteFiles))
	}
}

func TestSyncEngine_SymlinkEscapeProtection(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", tempDir)

	outsideDir := filepath.Join(tempDir, "Outside")
	_ = os.MkdirAll(outsideDir, 0755)
	secretFile := filepath.Join(outsideDir, "secret.key")
	_ = os.WriteFile(secretFile, []byte("SUPER_SECRET_KEY"), 0600)

	syncDir := filepath.Join(tempDir, "SyncFolder")
	_ = os.MkdirAll(syncDir, 0755)
	normalFile := filepath.Join(syncDir, "normal.txt")
	_ = os.WriteFile(normalFile, []byte("Normal content"), 0644)

	// Create symlink escaping syncDir
	escapingSymlink := filepath.Join(syncDir, "stolen_secret.key")
	_ = os.Symlink(secretFile, escapingSymlink)

	// Create symlink staying inside syncDir
	validSymlink := filepath.Join(syncDir, "valid_link.txt")
	_ = os.Symlink(normalFile, validSymlink)

	uploadedFiles := make(map[string]string)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			if r.URL.Path == "/api/info" {
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(`{
					"name": "SymlinkPhone",
					"folders": [
						{"id": "1", "name": "Docs", "path": "/storage/Docs", "is_sync": true}
					]
				}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
		case "PROPFIND":
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(http.StatusMultiStatus)
			_, _ = w.Write([]byte(`<multistatus xmlns="DAV:"></multistatus>`))
		case "PUT":
			body, _ := io.ReadAll(r.Body)
			uploadedFiles[strings.TrimPrefix(r.URL.Path, "/")] = string(body)
			w.WriteHeader(http.StatusCreated)
		}
	}))
	defer server.Close()

	parts := strings.Split(strings.TrimPrefix(server.URL, "http://"), ":")
	ip := parts[0]
	port, _ := strconv.Atoi(parts[1])

	_ = keyring.SetSyncMapping("SymlinkPhone", "Docs", syncDir)
	engine := NewSyncEngine(nil)

	stats, _, err := engine.SyncDevice("SymlinkPhone", ip, port, "token-123")
	if err != nil {
		t.Fatalf("SyncDevice failed: %v", err)
	}

	// Normal file and valid internal symlink should be uploaded
	if _, ok := uploadedFiles["Docs/normal.txt"]; !ok {
		t.Errorf("normal.txt should have been uploaded")
	}
	if _, ok := uploadedFiles["Docs/valid_link.txt"]; !ok {
		t.Errorf("valid_link.txt should have been uploaded")
	}

	// Escaping symlink MUST NOT be uploaded!
	if _, ok := uploadedFiles["Docs/stolen_secret.key"]; ok {
		t.Fatalf("SECURITY VIOLATION: escaping symlink was uploaded to remote!")
	}
	_ = stats
}

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

	"qsync/cli/internal/keyring"
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

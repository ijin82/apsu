package server

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"qsync/core/internal/auth"
	"qsync/core/internal/storage"
)

func setupTestEnv(t *testing.T) (*storage.Manager, *auth.PairingManager, *Server, string, string) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	folderA := filepath.Join(tempDir, "FolderA")
	folderB := filepath.Join(tempDir, "FolderB")

	if err := os.MkdirAll(folderA, 0755); err != nil {
		t.Fatalf("failed to create folderA: %v", err)
	}
	if err := os.MkdirAll(folderB, 0755); err != nil {
		t.Fatalf("failed to create folderB: %v", err)
	}

	// Create test file in folderA
	if err := os.WriteFile(filepath.Join(folderA, "hello.txt"), []byte("Hello qSync!"), 0644); err != nil {
		t.Fatalf("failed to write test file: %v", err)
	}

	store, err := storage.NewManager(configDir)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	// Use port 0 for random free port
	_ = store.SetServerSettings("Test Phone", 0)

	_, err = store.AddFolder("Docs", folderA)
	if err != nil {
		t.Fatalf("failed to add folder Docs: %v", err)
	}
	_, err = store.AddFolder("Photos", folderB)
	if err != nil {
		t.Fatalf("failed to add folder Photos: %v", err)
	}

	pairingMgr := auth.NewPairingManager(store)
	srv := NewServer(store, pairingMgr)

	return store, pairingMgr, srv, folderA, folderB
}

func TestServerPairingAndWebDAV(t *testing.T) {
	_, pairingMgr, srv, folderA, _ := setupTestEnv(t)

	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	port := srv.Port()
	if port <= 0 {
		t.Fatalf("expected valid port, got %d", port)
	}
	baseURL := "http://127.0.0.1:" + string(rune(port))
	baseURL = "http://127.0.0.1:" + httpPortString(port)

	// 1. Unauthenticated request to /Docs/hello.txt should return 401
	resp, err := http.Get(baseURL + "/Docs/hello.txt")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized, got %d", resp.StatusCode)
	}
	resp.Body.Close()

	// 2. Pair with PIN
	pin, _ := pairingMgr.GeneratePIN()
	pairReq := PairRequest{
		PIN:        pin,
		DeviceName: "Linux-Test-PC",
	}
	pairBody, _ := json.Marshal(pairReq)
	pairResp, err := http.Post(baseURL+"/api/pair", "application/json", bytes.NewReader(pairBody))
	if err != nil {
		t.Fatalf("pair request failed: %v", err)
	}
	if pairResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for pair, got %d", pairResp.StatusCode)
	}

	var pairData PairResponse
	if err := json.NewDecoder(pairResp.Body).Decode(&pairData); err != nil {
		t.Fatalf("failed to decode pair response: %v", err)
	}
	pairResp.Body.Close()

	if !pairData.Success || pairData.Token == "" {
		t.Fatalf("pairing failed or token empty: %+v", pairData)
	}
	if pairData.TLSPort <= 0 || pairData.CertFingerprint == "" {
		t.Fatalf("expected valid TLSPort and CertFingerprint in pair response, got port=%d fp=%s", pairData.TLSPort, pairData.CertFingerprint)
	}

	// 3. Access file using Basic Auth with Token
	client := &http.Client{}
	req, err := http.NewRequest("GET", baseURL+"/Docs/hello.txt", nil)
	if err != nil {
		t.Fatalf("failed to create request: %v", err)
	}
	req.SetBasicAuth("qsync", pairData.Token)

	getResp, err := client.Do(req)
	if err != nil {
		t.Fatalf("failed to get file: %v", err)
	}
	defer getResp.Body.Close()

	if getResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", getResp.StatusCode)
	}

	content, err := io.ReadAll(getResp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if string(content) != "Hello qSync!" {
		t.Fatalf("unexpected content: %s", string(content))
	}

	// 4. Test uploading a new file via WebDAV PUT
	uploadReq, err := http.NewRequest("PUT", baseURL+"/Docs/uploaded.txt", bytes.NewReader([]byte("Uploaded via WebDAV")))
	if err != nil {
		t.Fatalf("failed to create PUT request: %v", err)
	}
	uploadReq.SetBasicAuth("qsync", pairData.Token)
	uploadResp, err := client.Do(uploadReq)
	if err != nil {
		t.Fatalf("PUT request failed: %v", err)
	}
	uploadResp.Body.Close()
	if uploadResp.StatusCode != http.StatusCreated && uploadResp.StatusCode != http.StatusOK && uploadResp.StatusCode != http.StatusNoContent {
		t.Fatalf("expected 201/200/204 on PUT, got %d", uploadResp.StatusCode)
	}

	// Verify file was written to disk in folderA
	savedContent, err := os.ReadFile(filepath.Join(folderA, "uploaded.txt"))
	if err != nil {
		t.Fatalf("uploaded file does not exist on disk: %v", err)
	}
	if string(savedContent) != "Uploaded via WebDAV" {
		t.Fatalf("unexpected disk content: %s", string(savedContent))
	}

	// 5. Test /api/auth/verify with valid token
	verifyReq, err := http.NewRequest("GET", baseURL+"/api/auth/verify", nil)
	if err != nil {
		t.Fatalf("failed to create verify request: %v", err)
	}
	verifyReq.Header.Set("Authorization", "Bearer "+pairData.Token)
	verifyResp, err := client.Do(verifyReq)
	if err != nil {
		t.Fatalf("verify request failed: %v", err)
	}
	defer verifyResp.Body.Close()
	if verifyResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for valid token on /api/auth/verify, got %d", verifyResp.StatusCode)
	}

	// 6. Test /api/auth/verify with invalid token
	badVerifyReq, _ := http.NewRequest("GET", baseURL+"/api/auth/verify", nil)
	badVerifyReq.Header.Set("Authorization", "Bearer invalid-token-12345")
	badVerifyResp, err := client.Do(badVerifyReq)
	if err != nil {
		t.Fatalf("bad verify request failed: %v", err)
	}
	defer badVerifyResp.Body.Close()
	if badVerifyResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for invalid token on /api/auth/verify, got %d", badVerifyResp.StatusCode)
	}
}

func TestServerSecurityHardening(t *testing.T) {
	_, pairingMgr, srv, _, _ := setupTestEnv(t)
	if err := srv.Start(); err != nil {
		t.Fatalf("failed to start server: %v", err)
	}
	defer srv.Stop()

	baseURL := "http://127.0.0.1:" + httpPortString(srv.Port())
	client := &http.Client{}

	// 1. /api/pin MUST be removed: unauthenticated calls fall to WebDAV 401 (not API JSON)
	pinResp, err := client.Get(baseURL + "/api/pin")
	if err != nil {
		t.Fatalf("failed to query /api/pin: %v", err)
	}
	pinBody, _ := io.ReadAll(pinResp.Body)
	pinResp.Body.Close()
	if pinResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /api/pin, got %d", pinResp.StatusCode)
	}
	if strings.Contains(string(pinBody), "pin") {
		t.Fatalf("security violation: /api/pin returned PIN data: %s", string(pinBody))
	}

	// 2. /api/debug MUST be removed: unauthenticated calls fall to WebDAV 401 (not debug JSON)
	debugResp, err := client.Get(baseURL + "/api/debug")
	if err != nil {
		t.Fatalf("failed to query /api/debug: %v", err)
	}
	debugBody, _ := io.ReadAll(debugResp.Body)
	debugResp.Body.Close()
	if debugResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 for unauthenticated /api/debug, got %d", debugResp.StatusCode)
	}
	if strings.Contains(string(debugBody), "storage_root") || strings.Contains(string(debugBody), "sample_files") {
		t.Fatalf("security violation: /api/debug returned debug data: %s", string(debugBody))
	}

	// 3. /api/folders MUST reject unauthenticated GET, POST, DELETE with 401
	foldersGetResp, err := client.Get(baseURL + "/api/folders")
	if err != nil {
		t.Fatalf("failed to query /api/folders: %v", err)
	}
	foldersGetResp.Body.Close()
	if foldersGetResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for unauthenticated GET /api/folders, got %d", foldersGetResp.StatusCode)
	}

	foldersPostReq, _ := http.NewRequest("POST", baseURL+"/api/folders", bytes.NewReader([]byte(`{"name":"Evil","path":"/storage/emulated/0"}`)))
	foldersPostResp, err := client.Do(foldersPostReq)
	if err != nil {
		t.Fatalf("failed to POST /api/folders: %v", err)
	}
	foldersPostResp.Body.Close()
	if foldersPostResp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected 401 Unauthorized for unauthenticated POST /api/folders, got %d", foldersPostResp.StatusCode)
	}

	// Pair to get a valid token
	pin, _ := pairingMgr.GeneratePIN()
	token, err := pairingMgr.ValidatePINAndPair(pin, "Client-PC")
	if err != nil {
		t.Fatalf("pairing failed: %v", err)
	}

	// Authenticated GET /api/folders MUST succeed
	authFoldersReq, _ := http.NewRequest("GET", baseURL+"/api/folders", nil)
	authFoldersReq.Header.Set("Authorization", "Bearer "+token)
	authFoldersResp, err := client.Do(authFoldersReq)
	if err != nil {
		t.Fatalf("auth GET /api/folders failed: %v", err)
	}
	defer authFoldersResp.Body.Close()
	if authFoldersResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for authenticated /api/folders, got %d", authFoldersResp.StatusCode)
	}

	// Remote folder modification (POST / DELETE) MUST be blocked with 405 Method Not Allowed
	authPostReq, _ := http.NewRequest("POST", baseURL+"/api/folders", bytes.NewReader([]byte(`{"name":"Evil","path":"/storage/emulated/0"}`)))
	authPostReq.Header.Set("Authorization", "Bearer "+token)
	authPostResp, err := client.Do(authPostReq)
	if err != nil {
		t.Fatalf("auth POST /api/folders failed: %v", err)
	}
	authPostResp.Body.Close()
	if authPostResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed for POST /api/folders, got %d", authPostResp.StatusCode)
	}

	authDeleteReq, _ := http.NewRequest("DELETE", baseURL+"/api/folders?id=123", nil)
	authDeleteReq.Header.Set("Authorization", "Bearer "+token)
	authDeleteResp, err := client.Do(authDeleteReq)
	if err != nil {
		t.Fatalf("auth DELETE /api/folders failed: %v", err)
	}
	authDeleteResp.Body.Close()
	if authDeleteResp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 Method Not Allowed for DELETE /api/folders, got %d", authDeleteResp.StatusCode)
	}

	// 4. /api/info MUST NOT leak filesystem paths
	infoResp, err := client.Get(baseURL + "/api/info")
	if err != nil {
		t.Fatalf("failed to query /api/info: %v", err)
	}
	defer infoResp.Body.Close()
	if infoResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/info, got %d", infoResp.StatusCode)
	}
	infoBody, _ := io.ReadAll(infoResp.Body)

	// Parse as generic JSON map to check for presence of any "path" key inside folders
	var infoMap struct {
		Folders []map[string]interface{} `json:"folders"`
	}
	if err := json.Unmarshal(infoBody, &infoMap); err != nil {
		t.Fatalf("failed to parse /api/info JSON: %v", err)
	}
	if len(infoMap.Folders) == 0 {
		t.Fatalf("expected folders in /api/info")
	}
	for _, f := range infoMap.Folders {
		if pathVal, exists := f["path"]; exists && pathVal != "" {
			t.Fatalf("security violation: /api/info leaked internal filesystem path: %v", pathVal)
		}
	}

	// 5. Authenticated /api/folders MUST also omit internal filesystem path
	foldersReq, _ := http.NewRequest("GET", baseURL+"/api/folders", nil)
	foldersReq.Header.Set("Authorization", "Bearer "+token)
	foldersResp, err := client.Do(foldersReq)
	if err != nil {
		t.Fatalf("failed to query /api/folders: %v", err)
	}
	foldersBody, _ := io.ReadAll(foldersResp.Body)
	foldersResp.Body.Close()
	if foldersResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK for /api/folders, got %d", foldersResp.StatusCode)
	}

	var foldersList []map[string]interface{}
	if err := json.Unmarshal(foldersBody, &foldersList); err != nil {
		t.Fatalf("failed to parse /api/folders JSON: %v", err)
	}
	for _, f := range foldersList {
		if pathVal, exists := f["path"]; exists && pathVal != "" {
			t.Fatalf("security violation: /api/folders leaked internal filesystem path: %v", pathVal)
		}
	}

	// 6. Test Rate Limiting on repeated failed token verifications
	for i := 0; i < 20; i++ {
		reqBad, _ := http.NewRequest("GET", baseURL+"/api/auth/verify", nil)
		reqBad.Header.Set("Authorization", "Bearer invalid-token")
		respBad, err := client.Do(reqBad)
		if err == nil {
			respBad.Body.Close()
		}
	}

	// 21st attempt should be rate limited (429 Too Many Requests)
	reqRateLimited, _ := http.NewRequest("GET", baseURL+"/api/auth/verify", nil)
	reqRateLimited.Header.Set("Authorization", "Bearer invalid-token")
	respRateLimited, err := client.Do(reqRateLimited)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer respRateLimited.Body.Close()
	if respRateLimited.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests after 20 failed attempts, got %d", respRateLimited.StatusCode)
	}

	// 7. WebDAV request from rate-limited IP MUST return 429 Too Many Requests
	davReq, _ := http.NewRequest("GET", baseURL+"/Docs/hello.txt", nil)
	davResp, err := client.Do(davReq)
	if err != nil {
		t.Fatalf("WebDAV request failed: %v", err)
	}
	defer davResp.Body.Close()
	if davResp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 for rate-limited IP on WebDAV, got %d", davResp.StatusCode)
	}

	// 8. Test Rate Limiting on /api/pair
	for i := 0; i < 5; i++ {
		pReq := PairRequest{PIN: "000000", DeviceName: "Attacker"}
		b, _ := json.Marshal(pReq)
		r, _ := http.Post(baseURL+"/api/pair", "application/json", bytes.NewReader(b))
		if r != nil {
			r.Body.Close()
		}
	}

	// 6th pair attempt from same IP must be rate limited with 429
	pReq := PairRequest{PIN: "000000", DeviceName: "Attacker"}
	b, _ := json.Marshal(pReq)
	pResp, err := http.Post(baseURL+"/api/pair", "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("pair request failed: %v", err)
	}
	defer pResp.Body.Close()
	if pResp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("expected 429 Too Many Requests on /api/pair after 5 failed attempts, got %d", pResp.StatusCode)
	}
}

func httpPortString(port int) string {
	var buf []byte
	var i int
	for q := port; q > 0; q /= 10 {
		i++
	}
	buf = make([]byte, i)
	for q := port; q > 0; q /= 10 {
		i--
		buf[i] = byte('0' + (q % 10))
	}
	return string(buf)
}

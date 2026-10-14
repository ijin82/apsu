package client

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
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

type RemoteFolder struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Path   string `json:"path"`
	IsSync bool   `json:"is_sync"`
}

type ServerInfo struct {
	Name    string         `json:"name"`
	Version string         `json:"version"`
	Port    int            `json:"port"`
	Folders []RemoteFolder `json:"folders"`
}

// FetchServerInfo queries /api/info to retrieve device name and shared folders list
func FetchServerInfo(ip string, port int) (*ServerInfo, error) {
	httpClient := &http.Client{Timeout: 1500 * time.Millisecond}
	apiURL := fmt.Sprintf("http://%s:%d/api/info", ip, port)
	resp, err := httpClient.Get(apiURL)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned status: %d", resp.StatusCode)
	}

	var info ServerInfo
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return nil, err
	}
	return &info, nil
}

// FetchServerName queries /api/info to retrieve the actual device name set in phone settings
func FetchServerName(ip string, port int) string {
	info, err := FetchServerInfo(ip, port)
	if err == nil && info.Name != "" {
		return info.Name
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

type RemoteFileInfo struct {
	RelPath string
	IsDir   bool
	Size    int64
	ModTime time.Time
}

type davResponse struct {
	Href     string        `xml:"href"`
	PropStat []davPropStat `xml:"propstat"`
}

type davPropStat struct {
	Prop   davProp `xml:"prop"`
	Status string  `xml:"status"`
}

type davProp struct {
	ResourceType     davResourceType `xml:"resourcetype"`
	GetContentLength int64           `xml:"getcontentlength"`
	GetLastModified  string          `xml:"getlastmodified"`
}

type davResourceType struct {
	Collection *struct{} `xml:"collection"`
}

type davMultiStatus struct {
	XMLName   xml.Name      `xml:"multistatus"`
	Responses []davResponse `xml:"response"`
}

// ListRemoteTree recursively lists all files and directories in remoteFolder via PROPFIND.
func ListRemoteTree(ip string, port int, token string, folderName string) (map[string]RemoteFileInfo, error) {
	httpClient := &http.Client{Timeout: 30 * time.Second}
	folderURL := fmt.Sprintf("http://%s:%d/%s/", ip, port, url.PathEscape(folderName))

	req, err := http.NewRequest("PROPFIND", folderURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Depth", "infinity")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, os.ErrNotExist
	}
	if resp.StatusCode != http.StatusMultiStatus && resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("PROPFIND failed with status %d", resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var ms davMultiStatus
	if err := xml.Unmarshal(bodyBytes, &ms); err != nil {
		return nil, fmt.Errorf("failed to parse WebDAV XML: %w", err)
	}

	result := make(map[string]RemoteFileInfo)
	basePrefix := "/" + folderName + "/"
	basePrefixAlt := "/" + folderName

	for _, r := range ms.Responses {
		decodedHref, err := url.PathUnescape(r.Href)
		if err != nil {
			decodedHref = r.Href
		}

		cleanHref := path.Clean("/" + strings.TrimPrefix(decodedHref, "/"))
		var rel string
		if strings.HasPrefix(cleanHref, basePrefix) {
			rel = strings.TrimPrefix(cleanHref, basePrefix)
		} else if cleanHref == basePrefixAlt || cleanHref == strings.TrimSuffix(basePrefix, "/") {
			// This is the root folder itself
			continue
		} else {
			idx := strings.Index(cleanHref, "/"+folderName+"/")
			if idx >= 0 {
				rel = cleanHref[idx+len("/"+folderName+"/"):]
			} else {
				continue
			}
		}

		rel = strings.TrimPrefix(rel, "/")
		if rel == "" || rel == "." {
			continue
		}

		var isDir bool
		var size int64
		var modTime time.Time

		for _, ps := range r.PropStat {
			if strings.Contains(ps.Status, "200") {
				if ps.Prop.ResourceType.Collection != nil {
					isDir = true
				}
				size = ps.Prop.GetContentLength
				if ps.Prop.GetLastModified != "" {
					modTime, _ = http.ParseTime(ps.Prop.GetLastModified)
				}
				break
			}
		}

		result[rel] = RemoteFileInfo{
			RelPath: rel,
			IsDir:   isDir,
			Size:    size,
			ModTime: modTime,
		}
	}

	return result, nil
}

// UploadFile uploads a local file to the phone WebDAV server via HTTP PUT.
func UploadFile(ip string, port int, token string, remotePath string, localPath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()

	fi, err := file.Stat()
	if err != nil {
		return err
	}

	parts := strings.Split(strings.TrimPrefix(remotePath, "/"), "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	escapedPath := strings.Join(parts, "/")

	uploadURL := fmt.Sprintf("http://%s:%d/%s", ip, port, escapedPath)
	req, err := http.NewRequest("PUT", uploadURL, file)
	if err != nil {
		return err
	}
	req.ContentLength = fi.Size()
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/octet-stream")

	httpClient := &http.Client{Timeout: 10 * time.Minute}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("PUT %s failed: status %d", remotePath, resp.StatusCode)
	}
	return nil
}

// DeleteRemote deletes a file or directory on the phone WebDAV server via HTTP DELETE.
func DeleteRemote(ip string, port int, token string, remotePath string) error {
	parts := strings.Split(strings.TrimPrefix(remotePath, "/"), "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	escapedPath := strings.Join(parts, "/")

	deleteURL := fmt.Sprintf("http://%s:%d/%s", ip, port, escapedPath)
	req, err := http.NewRequest("DELETE", deleteURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("DELETE %s failed: status %d", remotePath, resp.StatusCode)
	}
	return nil
}

// MkcolRemote creates a directory on the phone WebDAV server via HTTP MKCOL.
func MkcolRemote(ip string, port int, token string, remotePath string) error {
	parts := strings.Split(strings.TrimPrefix(remotePath, "/"), "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	escapedPath := strings.Join(parts, "/")

	mkcolURL := fmt.Sprintf("http://%s:%d/%s", ip, port, escapedPath)
	req, err := http.NewRequest("MKCOL", mkcolURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)

	httpClient := &http.Client{Timeout: 30 * time.Second}
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusMethodNotAllowed && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("MKCOL %s failed: status %d", remotePath, resp.StatusCode)
	}
	return nil
}


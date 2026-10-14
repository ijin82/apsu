package storage

import (
	"net/url"
	"path/filepath"
	"strings"
)

// NormalizePath converts Android SAF content URIs (e.g. content://.../primary%3ADCIM)
// into standard POSIX paths (/storage/emulated/0/DCIM) accessible via MANAGE_EXTERNAL_STORAGE.
func NormalizePath(input string) string {
	raw := strings.TrimSpace(input)
	if raw == "" {
		return ""
	}

	// Try URL parsing
	var pathStr string
	if u, err := url.Parse(raw); err == nil && u.Scheme != "" {
		pathStr = u.Path
		// If URI is content://com.android.externalstorage.documents/tree/primary%3ADCIM
		if strings.Contains(raw, "tree/") {
			parts := strings.Split(raw, "tree/")
			if len(parts) > 1 {
				pathStr = parts[1]
			}
		} else if strings.Contains(raw, "document/") {
			parts := strings.Split(raw, "document/")
			if len(parts) > 1 {
				pathStr = parts[1]
			}
		}
	} else {
		pathStr = raw
	}

	// Decode URL escapes (e.g. %3A -> :, %20 -> space)
	if decoded, err := url.QueryUnescape(pathStr); err == nil {
		pathStr = decoded
	}

	// Check for primary storage prefix
	if idx := strings.Index(pathStr, "primary:"); idx != -1 {
		sub := strings.TrimPrefix(pathStr[idx+len("primary:"):], "/")
		return filepath.Clean(filepath.Join("/storage/emulated/0", sub))
	}

	if strings.HasPrefix(pathStr, "/tree/") {
		sub := strings.TrimPrefix(pathStr, "/tree/")
		if strings.HasPrefix(sub, "primary:") {
			return filepath.Clean(filepath.Join("/storage/emulated/0", strings.TrimPrefix(sub, "primary:")))
		}
	}

	if strings.HasPrefix(pathStr, "/document/") {
		sub := strings.TrimPrefix(pathStr, "/document/")
		if strings.HasPrefix(sub, "primary:") {
			return filepath.Clean(filepath.Join("/storage/emulated/0", strings.TrimPrefix(sub, "primary:")))
		}
	}

	// Clean standard file path
	clean := filepath.Clean(pathStr)
	return clean
}

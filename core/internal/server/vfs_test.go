package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"qsync/core/internal/storage"
)

func TestVFSSandboxAndSymlinkEscape(t *testing.T) {
	tempDir := t.TempDir()
	configDir := filepath.Join(tempDir, "config")
	sharedDir := filepath.Join(tempDir, "SharedDocs")
	outsideDir := filepath.Join(tempDir, "SecretOutside")

	if err := os.MkdirAll(sharedDir, 0755); err != nil {
		t.Fatalf("failed to create sharedDir: %v", err)
	}
	if err := os.MkdirAll(outsideDir, 0755); err != nil {
		t.Fatalf("failed to create outsideDir: %v", err)
	}

	// Create secret file outside shared directory
	secretFile := filepath.Join(outsideDir, "shadow.txt")
	if err := os.WriteFile(secretFile, []byte("SUPER_SECRET_DATA"), 0600); err != nil {
		t.Fatalf("failed to write secretFile: %v", err)
	}

	// Create a symlink inside sharedDir pointing to outsideDir
	symlinkToOutside := filepath.Join(sharedDir, "symlink_escape")
	if err := os.Symlink(outsideDir, symlinkToOutside); err != nil {
		t.Skipf("symlinks not supported on this OS: %v", err)
	}

	// Create a symlink directly to secretFile
	symlinkToFile := filepath.Join(sharedDir, "stolen_file.txt")
	if err := os.Symlink(secretFile, symlinkToFile); err != nil {
		t.Skipf("symlinks not supported on this OS: %v", err)
	}

	// Create legitimate file inside sharedDir
	legitFile := filepath.Join(sharedDir, "normal.txt")
	if err := os.WriteFile(legitFile, []byte("Normal public content"), 0644); err != nil {
		t.Fatalf("failed to write legitFile: %v", err)
	}

	store, err := storage.NewManager(configDir)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	_, err = store.AddFolder("Docs", sharedDir)
	if err != nil {
		t.Fatalf("failed to add folder: %v", err)
	}

	vfs := NewVirtualFS(store)
	ctx := context.Background()

	// 1. Legitimate file should be readable
	f, err := vfs.OpenFile(ctx, "/Docs/normal.txt", os.O_RDONLY, 0)
	if err != nil {
		t.Fatalf("failed to open legit file: %v", err)
	}
	buf := make([]byte, 64)
	n, _ := f.Read(buf)
	f.Close()
	if string(buf[:n]) != "Normal public content" {
		t.Fatalf("unexpected content: %s", string(buf[:n]))
	}

	// 2. Reading secret file through direct symlink MUST return os.ErrPermission
	_, err = vfs.OpenFile(ctx, "/Docs/stolen_file.txt", os.O_RDONLY, 0)
	if err != os.ErrPermission {
		t.Fatalf("expected os.ErrPermission for symlink to outside file, got: %v", err)
	}

	// 3. Stat on symlink escaping directory MUST return os.ErrPermission
	_, err = vfs.Stat(ctx, "/Docs/stolen_file.txt")
	if err != os.ErrPermission {
		t.Fatalf("expected os.ErrPermission for Stat on escaping symlink, got: %v", err)
	}

	// 4. Accessing file through directory symlink (/Docs/symlink_escape/shadow.txt) MUST return os.ErrPermission
	_, err = vfs.OpenFile(ctx, "/Docs/symlink_escape/shadow.txt", os.O_RDONLY, 0)
	if err != os.ErrPermission {
		t.Fatalf("expected os.ErrPermission for traversal through escaping dir symlink, got: %v", err)
	}

	// 5. Creating directory or file through escaping symlink MUST return os.ErrPermission
	err = vfs.Mkdir(ctx, "/Docs/symlink_escape/newdir", 0755)
	if err != os.ErrPermission {
		t.Fatalf("expected os.ErrPermission for Mkdir through escaping symlink, got: %v", err)
	}

	// 6. RemoveAll on escaping symlink target MUST return os.ErrPermission
	err = vfs.RemoveAll(ctx, "/Docs/symlink_escape/shadow.txt")
	if err != os.ErrPermission {
		t.Fatalf("expected os.ErrPermission for RemoveAll through escaping symlink, got: %v", err)
	}
}

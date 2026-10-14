package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSyncLoggerRotation(t *testing.T) {
	tempDir := t.TempDir()
	logPath := filepath.Join(tempDir, "sync_history.log")

	logger, err := NewSyncLogger(logPath)
	if err != nil {
		t.Fatalf("failed to create logger: %v", err)
	}

	// Add 20 sessions
	for i := 1; i <= 20; i++ {
		logger.StartSession(fmt.Sprintf("Device_%d", i))
		logger.Log("Music", "CREATE FILE", fmt.Sprintf("track_%d.mp3", i), "1024 bytes")
		logger.EndSession("SUCCESS", fmt.Sprintf("Session %d completed", i))
	}

	content, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("failed to read log: %v", err)
	}

	text := string(content)
	count := strings.Count(text, SessionHeaderPrefix)
	if count != MaxSyncSessions {
		t.Fatalf("expected exactly %d sessions, got %d", MaxSyncSessions, count)
	}

	// Verify that the oldest sessions (1 to 5) are rotated out and session 20 is present
	if strings.Contains(text, "Device_1]") {
		t.Fatalf("Device_1 should have been rotated out")
	}
	if strings.Contains(text, "Device_5]") {
		t.Fatalf("Device_5 should have been rotated out")
	}
	if !strings.Contains(text, "Device_6]") {
		t.Fatalf("Device_6 should be present")
	}
	if !strings.Contains(text, "Device_20]") {
		t.Fatalf("Device_20 should be present")
	}
}

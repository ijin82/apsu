package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const (
	MaxSyncSessions     = 15
	SessionHeaderPrefix = "=== SYNC SESSION:"
	SessionFooterPrefix = "=== END SYNC SESSION"
)

type SyncLogger struct {
	mu      sync.Mutex
	logPath string
	buffer  strings.Builder
}

func GetLogPath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		configDir = filepath.Join(home, ".config")
	}
	dir := filepath.Join(configDir, "qsync")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "sync_history.log"), nil
}

func NewSyncLogger(customPath string) (*SyncLogger, error) {
	path := customPath
	if path == "" {
		var err error
		path, err = GetLogPath()
		if err != nil {
			return nil, err
		}
	}
	return &SyncLogger{
		logPath: path,
	}, nil
}

func (l *SyncLogger) StartSession(deviceName string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.buffer.Reset()
	now := time.Now().Format("2006-01-02 15:04:05")
	l.buffer.WriteString(fmt.Sprintf("%s %s [Device: %s] ===\n", SessionHeaderPrefix, now, deviceName))
}

func (l *SyncLogger) Log(folder, opType, itemPath, details string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().Format("15:04:05")
	if details != "" {
		l.buffer.WriteString(fmt.Sprintf("[%s] [%s] %s \"%s\" (%s)\n", now, folder, opType, itemPath, details))
	} else {
		l.buffer.WriteString(fmt.Sprintf("[%s] [%s] %s \"%s\"\n", now, folder, opType, itemPath))
	}
}

func (l *SyncLogger) LogInfo(folder, message string) {
	l.mu.Lock()
	defer l.mu.Unlock()

	now := time.Now().Format("15:04:05")
	l.buffer.WriteString(fmt.Sprintf("[%s] [%s] %s\n", now, folder, message))
}

func (l *SyncLogger) EndSession(status string, summary string) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	l.buffer.WriteString(fmt.Sprintf("%s (%s: %s) ===\n\n", SessionFooterPrefix, status, summary))
	newSessionText := l.buffer.String()
	l.buffer.Reset()

	return l.appendAndRotate(newSessionText)
}

func (l *SyncLogger) appendAndRotate(newSessionText string) error {
	var existingContent string
	if data, err := os.ReadFile(l.logPath); err == nil {
		existingContent = string(data)
	}

	rawSessions := strings.Split(existingContent, SessionHeaderPrefix)
	var sessions []string
	for _, s := range rawSessions {
		trimmed := strings.TrimSpace(s)
		if trimmed != "" {
			sessions = append(sessions, SessionHeaderPrefix+s+"\n")
		}
	}

	sessions = append(sessions, newSessionText)

	if len(sessions) > MaxSyncSessions {
		sessions = sessions[len(sessions)-MaxSyncSessions:]
	}

	finalContent := strings.Join(sessions, "")
	return os.WriteFile(l.logPath, []byte(finalContent), 0600)
}

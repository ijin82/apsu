package sync

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"qsync/cli/internal/client"
	"qsync/cli/internal/keyring"
)

type SyncStats struct {
	CreatedCount int
	UpdatedCount int
	DeletedCount int
	SkippedCount int
	ErrorCount   int
	Duration     time.Duration
}

type SyncEngine struct {
	logger *SyncLogger
}

func NewSyncEngine(logger *SyncLogger) *SyncEngine {
	return &SyncEngine{
		logger: logger,
	}
}

// SyncDevice synchronizes all configured syncable folders for a given device.
func (e *SyncEngine) SyncDevice(deviceName, ip string, port int, token string) (SyncStats, string, error) {
	startTime := time.Now()
	var totalStats SyncStats

	if e.logger != nil {
		e.logger.StartSession(deviceName)
	}

	info, err := client.FetchServerInfo(ip, port)
	if err != nil {
		if e.logger != nil {
			_ = e.logger.EndSession("FAILED", fmt.Sprintf("Failed to fetch device info: %v", err))
		}
		return totalStats, "", fmt.Errorf("failed to fetch device info from %s:%d: %w", ip, port, err)
	}

	// Filter syncable folders
	var syncFolders []client.RemoteFolder
	var syncFolderNames []string
	for _, f := range info.Folders {
		if f.IsSync {
			syncFolders = append(syncFolders, f)
			syncFolderNames = append(syncFolderNames, f.Name)
		}
	}

	if len(syncFolders) == 0 {
		msg := "No syncable folders found on phone"
		if e.logger != nil {
			_ = e.logger.EndSession("SKIPPED", msg)
		}
		return totalStats, msg, nil
	}

	// Ensure any unmapped folders have template entries in config
	_ = keyring.EnsureSyncFoldersInConfig(deviceName, syncFolderNames)
	mappings := keyring.GetSyncMappings(deviceName)

	var summaries []string

	for _, rf := range syncFolders {
		localPath := mappings[rf.Name]
		if localPath == "" {
			if e.logger != nil {
				e.logger.LogInfo(rf.Name, "SKIPPED: No local source folder configured in client_config.json")
			}
			totalStats.SkippedCount++
			continue
		}

		cleanLocal := filepath.Clean(localPath)
		homeDir, _ := os.UserHomeDir()
		if cleanLocal == "/" || cleanLocal == "/home" || cleanLocal == homeDir ||
			cleanLocal == "/etc" || cleanLocal == "/usr" || cleanLocal == "/var" ||
			cleanLocal == "/bin" || cleanLocal == "/boot" || cleanLocal == "/root" {
			if e.logger != nil {
				e.logger.LogInfo(rf.Name, fmt.Sprintf("REFUSED: '%s' is a protected system/root path and cannot be mapped as sync source", cleanLocal))
			}
			totalStats.SkippedCount++
			continue
		}

		fi, err := os.Stat(cleanLocal)
		if err != nil || !fi.IsDir() {
			if e.logger != nil {
				e.logger.LogInfo(rf.Name, fmt.Sprintf("SKIPPED: Local folder '%s' does not exist or is not a directory", cleanLocal))
			}
			totalStats.SkippedCount++
			continue
		}

		stats, folderErr := e.syncSingleFolder(deviceName, ip, port, token, rf.Name, cleanLocal)
		totalStats.CreatedCount += stats.CreatedCount
		totalStats.UpdatedCount += stats.UpdatedCount
		totalStats.DeletedCount += stats.DeletedCount
		totalStats.ErrorCount += stats.ErrorCount

		if folderErr != nil {
			summaries = append(summaries, fmt.Sprintf("%s (Error: %v)", rf.Name, folderErr))
			if !client.Ping(ip, port) {
				break
			}
		} else {
			summaries = append(summaries, fmt.Sprintf("%s (+%d ~%d -%d)", rf.Name, stats.CreatedCount, stats.UpdatedCount, stats.DeletedCount))
		}
	}

	totalStats.Duration = time.Since(startTime)
	detailStr := ""
	if len(summaries) > 0 {
		detailStr = " [" + strings.Join(summaries, ", ") + "]"
	}
	summaryMsg := fmt.Sprintf("%d created, %d updated, %d deleted, %d skipped%s (%s)",
		totalStats.CreatedCount, totalStats.UpdatedCount, totalStats.DeletedCount, totalStats.SkippedCount, detailStr, totalStats.Duration.Round(10*time.Millisecond))

	status := "SUCCESS"
	if totalStats.ErrorCount > 0 {
		status = "WARNING"
	}

	if e.logger != nil {
		_ = e.logger.EndSession(status, summaryMsg)
	}

	return totalStats, summaryMsg, nil
}

func (e *SyncEngine) syncSingleFolder(deviceName, ip string, port int, token string, remoteFolder, localRoot string) (SyncStats, error) {
	var stats SyncStats

	if e.logger != nil {
		e.logger.LogInfo(remoteFolder, fmt.Sprintf("Source: %s -> Target: %s", localRoot, remoteFolder))
	}

	// 1. Scan local tree
	localFiles := make(map[string]fs.FileInfo)
	localDirs := make(map[string]bool)

	err := filepath.WalkDir(localRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(localRoot, p)
		if err != nil || rel == "." {
			return nil
		}
		// Convert Windows slashes to WebDAV standard /
		rel = filepath.ToSlash(rel)

		if d.IsDir() {
			localDirs[rel] = true
		} else {
			info, err := d.Info()
			if err == nil {
				localFiles[rel] = info
			}
		}
		return nil
	})
	if err != nil {
		if e.logger != nil {
			e.logger.LogInfo(remoteFolder, fmt.Sprintf("Error scanning local directory: %v", err))
		}
		return stats, err
	}

	// 2. Fetch remote tree
	remoteItems, err := client.ListRemoteTree(ip, port, token, remoteFolder)
	if err != nil {
		if e.logger != nil {
			e.logger.LogInfo(remoteFolder, fmt.Sprintf("Error fetching remote folder '%s': %v", remoteFolder, err))
		}
		return stats, err
	}

	remoteFiles := make(map[string]client.RemoteFileInfo)
	remoteDirs := make(map[string]bool)
	for rel, item := range remoteItems {
		if item.IsDir {
			remoteDirs[rel] = true
		} else {
			remoteFiles[rel] = item
		}
	}

	// 3. Create missing directories on remote
	// Sort directory keys to ensure parent directories are created first
	sortedDirs := make([]string, 0, len(localDirs))
	for d := range localDirs {
		sortedDirs = append(sortedDirs, d)
	}
	sort.Strings(sortedDirs)

	for _, d := range sortedDirs {
		if !remoteDirs[d] {
			remoteDirPath := remoteFolder + "/" + d
			if err := client.MkcolRemote(ip, port, token, remoteDirPath); err != nil {
				stats.ErrorCount++
				if e.logger != nil {
					e.logger.Log(remoteFolder, "MKCOL ERROR", d, err.Error())
				}
			} else {
				remoteDirs[d] = true
				if e.logger != nil {
					e.logger.Log(remoteFolder, "CREATE DIR", d, "")
				}
			}
		}
	}

	// 4. Upload missing or changed files
	for rel, localFi := range localFiles {
		remFi, exists := remoteFiles[rel]
		needsUpload := false
		opType := "CREATE FILE"

		if !exists {
			needsUpload = true
			opType = "CREATE FILE"
		} else if remFi.Size != localFi.Size() || localFi.ModTime().After(remFi.ModTime.Add(2*time.Second)) {
			needsUpload = true
			opType = "UPDATE FILE"
		}

		if needsUpload {
			localFilePath := filepath.Join(localRoot, filepath.FromSlash(rel))
			remoteFilePath := remoteFolder + "/" + rel

			if err := client.UploadFile(ip, port, token, remoteFilePath, localFilePath); err != nil {
				stats.ErrorCount++
				if e.logger != nil {
					e.logger.Log(remoteFolder, opType+" ERROR", rel, err.Error())
				}
				// If device became unreachable (connection refused, timeout, network error), abort immediately
				if !client.Ping(ip, port) {
					if e.logger != nil {
						e.logger.LogInfo(remoteFolder, "ABORTING: Connection to device lost during upload")
					}
					return stats, fmt.Errorf("connection to device lost: %w", err)
				}
			} else {
				if opType == "CREATE FILE" {
					stats.CreatedCount++
				} else {
					stats.UpdatedCount++
				}
				if e.logger != nil {
					e.logger.Log(remoteFolder, opType, rel, fmt.Sprintf("%d bytes", localFi.Size()))
				}
			}
		}
	}

	// 5. Delete remote files that do not exist locally
	for rel := range remoteFiles {
		if _, exists := localFiles[rel]; !exists {
			remoteFilePath := remoteFolder + "/" + rel
			if err := client.DeleteRemote(ip, port, token, remoteFilePath); err != nil {
				stats.ErrorCount++
				if e.logger != nil {
					e.logger.Log(remoteFolder, "DELETE FILE ERROR", rel, err.Error())
				}
			} else {
				stats.DeletedCount++
				if e.logger != nil {
					e.logger.Log(remoteFolder, "DELETE FILE", rel, "")
				}
			}
		}
	}

	// 6. Delete remote directories that do not exist locally
	// Sort in reverse order so children directories are deleted before parents
	sortedRemoteDirs := make([]string, 0, len(remoteDirs))
	for d := range remoteDirs {
		sortedRemoteDirs = append(sortedRemoteDirs, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(sortedRemoteDirs)))

	for _, d := range sortedRemoteDirs {
		if !localDirs[d] {
			remoteDirPath := remoteFolder + "/" + d
			if err := client.DeleteRemote(ip, port, token, remoteDirPath); err != nil {
				stats.ErrorCount++
				if e.logger != nil {
					e.logger.Log(remoteFolder, "DELETE DIR ERROR", d, err.Error())
				}
			} else {
				stats.DeletedCount++
				if e.logger != nil {
					e.logger.Log(remoteFolder, "DELETE DIR", d, "")
				}
			}
		}
	}

	return stats, nil
}

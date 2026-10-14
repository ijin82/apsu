package mount

import (
	"bytes"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type Mounter interface {
	Mount(ip string, port int, token string) error
	Unmount(ip string, port int) error
	IsMounted(ip string, port int) (bool, error)
	OpenInFileManager(ip string, port int) error
}

func GetMounter() Mounter {
	switch runtime.GOOS {
	case "darwin":
		return &darwinMounter{}
	case "windows":
		return &windowsMounter{}
	default:
		return &linuxMounter{}
	}
}

// Linux Mounter (GVFS / gio mount)
type linuxMounter struct{}

func (l *linuxMounter) Mount(ip string, port int, token string) error {
	uri := fmt.Sprintf("dav://qsync@%s:%d/", ip, port)
	cmd := exec.Command("gio", "mount", uri)
	cmd.Stdin = strings.NewReader(token + "\n")
	out, err := cmd.CombinedOutput()
	outputStr := string(out)

	if err != nil {
		if strings.Contains(outputStr, "already mounted") {
			return nil
		}
		return fmt.Errorf("gio mount failed: %s (%w)", strings.TrimSpace(outputStr), err)
	}
	return nil
}

func (l *linuxMounter) Unmount(ip string, port int) error {
	patterns := []string{
		fmt.Sprintf("dav://qsync@%s:%d/", ip, port),
		fmt.Sprintf("dav://%s:%d/", ip, port),
	}

	var lastErr error
	for _, p := range patterns {
		// Use -f to forcefully detach dead/offline network mounts without hanging
		cmd := exec.Command("gio", "mount", "-u", "-f", p)
		out, err := cmd.CombinedOutput()
		if err == nil {
			return nil
		}
		outStr := string(out)
		if strings.Contains(outStr, "not mounted") {
			continue
		}
		lastErr = fmt.Errorf("%s", strings.TrimSpace(outStr))
	}
	return lastErr
}

func (l *linuxMounter) IsMounted(ip string, port int) (bool, error) {
	cmd := exec.Command("gio", "mount", "-l")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	target := fmt.Sprintf("%s:%d", ip, port)
	targetHost := fmt.Sprintf("host=%s", ip)
	s := string(out)
	return strings.Contains(s, target) || strings.Contains(s, targetHost), nil
}

func (l *linuxMounter) OpenInFileManager(ip string, port int) error {
	uri := fmt.Sprintf("dav://qsync@%s:%d/", ip, port)
	cmd := exec.Command("gio", "open", uri)
	if err := cmd.Start(); err != nil {
		// Fallback to xdg-open
		return exec.Command("xdg-open", uri).Start()
	}
	return nil
}

// macOS Mounter (Finder / mount_webdav)
type darwinMounter struct{}

func (d *darwinMounter) Mount(ip string, port int, token string) error {
	url := fmt.Sprintf("http://qsync:%s@%s:%d/", token, ip, port)
	cmd := exec.Command("osascript", "-e", fmt.Sprintf(`mount volume "%s"`, url))
	return cmd.Run()
}

func (d *darwinMounter) Unmount(ip string, port int) error {
	cmd := exec.Command("diskutil", "unmount", "/Volumes/qSync")
	return cmd.Run()
}

func (d *darwinMounter) IsMounted(ip string, port int) (bool, error) {
	cmd := exec.Command("mount")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return bytes.Contains(out, []byte(fmt.Sprintf("%s:%d", ip, port))), nil
}

func (d *darwinMounter) OpenInFileManager(ip string, port int) error {
	return exec.Command("open", "/Volumes/qSync").Run()
}

// Windows Mounter (net use)
type windowsMounter struct{}

func (w *windowsMounter) Mount(ip string, port int, token string) error {
	url := fmt.Sprintf("http://%s:%d/", ip, port)
	cmd := exec.Command("net", "use", "*", url, token, "/user:qsync")
	return cmd.Run()
}

func (w *windowsMounter) Unmount(ip string, port int) error {
	url := fmt.Sprintf("http://%s:%d/", ip, port)
	cmd := exec.Command("net", "use", url, "/delete", "/y")
	return cmd.Run()
}

func (w *windowsMounter) IsMounted(ip string, port int) (bool, error) {
	cmd := exec.Command("net", "use")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return bytes.Contains(out, []byte(fmt.Sprintf("%s:%d", ip, port))), nil
}

func (w *windowsMounter) OpenInFileManager(ip string, port int) error {
	url := fmt.Sprintf("http://%s:%d/", ip, port)
	return exec.Command("explorer.exe", url).Run()
}

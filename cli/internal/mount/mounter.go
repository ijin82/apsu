package mount

import (
	"bytes"
	"fmt"
	"net"
	"os/exec"
	"runtime"
	"strings"
)

func validateHostAndPort(ip string, port int) error {
	if port <= 0 || port > 65535 {
		return fmt.Errorf("invalid port: %d", port)
	}
	if net.ParseIP(ip) != nil {
		return nil
	}
	// Check hostname syntax (RFC 1123)
	if len(ip) == 0 || len(ip) > 255 {
		return fmt.Errorf("invalid host length: %d", len(ip))
	}
	for _, part := range strings.Split(ip, ".") {
		if len(part) == 0 || len(part) > 63 {
			return fmt.Errorf("invalid host format: %s", ip)
		}
		for _, c := range part {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return fmt.Errorf("invalid host character %q in %s", c, ip)
			}
		}
	}
	return nil
}

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
	if err := validateHostAndPort(ip, port); err != nil {
		return err
	}
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
	if err := validateHostAndPort(ip, port); err != nil {
		return err
	}
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
	if err := validateHostAndPort(ip, port); err != nil {
		return false, err
	}
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
	if err := validateHostAndPort(ip, port); err != nil {
		return err
	}
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
	if err := validateHostAndPort(ip, port); err != nil {
		return err
	}
	cleanToken := strings.ReplaceAll(token, `\`, `\\`)
	cleanToken = strings.ReplaceAll(cleanToken, `"`, `\"`)
	url := fmt.Sprintf("http://qsync:%s@%s:%d/", cleanToken, ip, port)
	cmd := exec.Command("osascript", "-e", fmt.Sprintf(`mount volume "%s"`, url))
	return cmd.Run()
}

func (d *darwinMounter) Unmount(ip string, port int) error {
	cmd := exec.Command("diskutil", "unmount", "/Volumes/qSync")
	return cmd.Run()
}

func (d *darwinMounter) IsMounted(ip string, port int) (bool, error) {
	if err := validateHostAndPort(ip, port); err != nil {
		return false, err
	}
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
	if err := validateHostAndPort(ip, port); err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s:%d/", ip, port)
	cmd := exec.Command("net", "use", "*", url, token, "/user:qsync")
	return cmd.Run()
}

func (w *windowsMounter) Unmount(ip string, port int) error {
	if err := validateHostAndPort(ip, port); err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s:%d/", ip, port)
	cmd := exec.Command("net", "use", url, "/delete", "/y")
	return cmd.Run()
}

func (w *windowsMounter) IsMounted(ip string, port int) (bool, error) {
	if err := validateHostAndPort(ip, port); err != nil {
		return false, err
	}
	cmd := exec.Command("net", "use")
	out, err := cmd.Output()
	if err != nil {
		return false, err
	}
	return bytes.Contains(out, []byte(fmt.Sprintf("%s:%d", ip, port))), nil
}

func (w *windowsMounter) OpenInFileManager(ip string, port int) error {
	if err := validateHostAndPort(ip, port); err != nil {
		return err
	}
	url := fmt.Sprintf("http://%s:%d/", ip, port)
	return exec.Command("explorer.exe", url).Run()
}

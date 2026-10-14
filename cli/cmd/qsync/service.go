package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

const systemdUnitTemplate = `[Unit]
Description=qSync Background D-Bus Daemon
After=network.target

[Service]
Type=simple
ExecStart=%s daemon
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
`

func getServicePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".config", "systemd", "user")
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", err
	}
	return filepath.Join(dir, "qsync.service"), nil
}

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage qsync systemd user service",
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and enable qsync systemd user service",
	Run: func(cmd *cobra.Command, args []string) {
		execPath, err := os.Executable()
		if err != nil {
			fmt.Printf("❌ Failed to determine executable path: %v\n", err)
			return
		}
		execPath, err = filepath.EvalSymlinks(execPath)
		if err != nil {
			fmt.Printf("❌ Failed to resolve executable symlinks: %v\n", err)
			return
		}

		servicePath, err := getServicePath()
		if err != nil {
			fmt.Printf("❌ Failed to resolve systemd directory: %v\n", err)
			return
		}

		content := fmt.Sprintf(systemdUnitTemplate, execPath)
		if err := os.WriteFile(servicePath, []byte(content), 0644); err != nil {
			fmt.Printf("❌ Failed to write unit file: %v\n", err)
			return
		}

		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		if err := exec.Command("systemctl", "--user", "enable", "qsync.service").Run(); err != nil {
			fmt.Printf("⚠️ Unit created at %s, but enable failed: %v\n", servicePath, err)
			return
		}

		fmt.Printf("✅ Successfully installed systemd user service: %s\n", servicePath)
		fmt.Println("To start the service now, run: qsync service start")
	},
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop, disable, and remove qsync systemd user service",
	Run: func(cmd *cobra.Command, args []string) {
		_ = exec.Command("systemctl", "--user", "stop", "qsync.service").Run()
		_ = exec.Command("systemctl", "--user", "disable", "qsync.service").Run()

		servicePath, err := getServicePath()
		if err == nil {
			_ = os.Remove(servicePath)
		}
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		fmt.Println("✅ Successfully uninstalled qsync systemd service.")
	},
}

var serviceStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start qsync systemd user service",
	Run: func(cmd *cobra.Command, args []string) {
		cmdExec := exec.Command("systemctl", "--user", "start", "qsync.service")
		out, err := cmdExec.CombinedOutput()
		if err != nil {
			fmt.Printf("❌ Failed to start service: %s (%v)\n", string(out), err)
			return
		}
		fmt.Println("✅ Started qsync systemd service.")
	},
}

var serviceStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop qsync systemd user service",
	Run: func(cmd *cobra.Command, args []string) {
		cmdExec := exec.Command("systemctl", "--user", "stop", "qsync.service")
		out, err := cmdExec.CombinedOutput()
		if err != nil {
			fmt.Printf("❌ Failed to stop service: %s (%v)\n", string(out), err)
			return
		}
		fmt.Println("✅ Stopped qsync systemd service.")
	},
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show qsync systemd user service status",
	Run: func(cmd *cobra.Command, args []string) {
		cmdExec := exec.Command("systemctl", "--user", "status", "qsync.service")
		cmdExec.Stdout = os.Stdout
		cmdExec.Stderr = os.Stderr
		_ = cmdExec.Run()
	},
}

func init() {
	serviceCmd.AddCommand(serviceInstallCmd)
	serviceCmd.AddCommand(serviceUninstallCmd)
	serviceCmd.AddCommand(serviceStartCmd)
	serviceCmd.AddCommand(serviceStopCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
	rootCmd.AddCommand(serviceCmd)
}

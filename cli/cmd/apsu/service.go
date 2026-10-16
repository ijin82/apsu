package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

const systemdUnitTemplate = `[Unit]
Description=Apsu Background D-Bus Daemon
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
	return filepath.Join(dir, "apsu.service"), nil
}

var serviceCmd = &cobra.Command{
	Use:   "service",
	Short: "Manage apsu systemd user service",
}

var serviceInstallCmd = &cobra.Command{
	Use:   "install",
	Short: "Install and enable apsu systemd user service",
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
		if err := exec.Command("systemctl", "--user", "enable", "apsu.service").Run(); err != nil {
			fmt.Printf("⚠️ Unit created at %s, but enable failed: %v\n", servicePath, err)
			return
		}

		fmt.Printf("✅ Successfully installed systemd user service: %s\n", servicePath)
		fmt.Println("To start the service now, run: apsu service start")
	},
}

var serviceUninstallCmd = &cobra.Command{
	Use:   "uninstall",
	Short: "Stop, disable, and remove apsu systemd user service",
	Run: func(cmd *cobra.Command, args []string) {
		_ = exec.Command("systemctl", "--user", "stop", "apsu.service").Run()
		_ = exec.Command("systemctl", "--user", "disable", "apsu.service").Run()

		servicePath, err := getServicePath()
		if err == nil {
			_ = os.Remove(servicePath)
		}
		_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
		fmt.Println("✅ Successfully uninstalled apsu systemd service.")
	},
}

var serviceStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start apsu systemd user service",
	Run: func(cmd *cobra.Command, args []string) {
		cmdExec := exec.Command("systemctl", "--user", "start", "apsu.service")
		out, err := cmdExec.CombinedOutput()
		if err != nil {
			fmt.Printf("❌ Failed to start service: %s (%v)\n", string(out), err)
			return
		}
		fmt.Println("✅ Started apsu systemd service.")
	},
}

var serviceStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop apsu systemd user service",
	Run: func(cmd *cobra.Command, args []string) {
		cmdExec := exec.Command("systemctl", "--user", "stop", "apsu.service")
		out, err := cmdExec.CombinedOutput()
		if err != nil {
			fmt.Printf("❌ Failed to stop service: %s (%v)\n", string(out), err)
			return
		}
		fmt.Println("✅ Stopped apsu systemd service.")
	},
}

var serviceRestartCmd = &cobra.Command{
	Use:   "restart",
	Short: "Restart apsu systemd user service",
	Run: func(cmd *cobra.Command, args []string) {
		cmdExec := exec.Command("systemctl", "--user", "restart", "apsu.service")
		out, err := cmdExec.CombinedOutput()
		if err != nil {
			fmt.Printf("❌ Failed to restart service: %s (%v)\n", string(out), err)
			return
		}
		fmt.Println("✅ Restarted apsu systemd service.")
	},
}

var serviceStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show apsu systemd user service status",
	Run: func(cmd *cobra.Command, args []string) {
		cmdExec := exec.Command("systemctl", "--user", "status", "apsu.service")
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
	serviceCmd.AddCommand(serviceRestartCmd)
	serviceCmd.AddCommand(serviceStatusCmd)
	rootCmd.AddCommand(serviceCmd)
}

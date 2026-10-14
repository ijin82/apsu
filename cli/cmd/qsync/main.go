package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"github.com/spf13/cobra"

	"qsync/cli/internal/client"
	"qsync/cli/internal/dbus"
	"qsync/cli/internal/discovery"
	"qsync/cli/internal/keyring"
	"qsync/cli/internal/mount"
)

var rootCmd = &cobra.Command{
	Use:   "qsync",
	Short: "qSync - Quick Sync CLI for Android and GNOME/Linux",
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan local network for active qSync phones",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔍 Scanning local network for qSync devices (2s)...")
		devices, err := discovery.Scan(2 * time.Second)
		if err != nil {
			fmt.Printf("❌ Scan error: %v\n", err)
			return
		}
		if len(devices) == 0 {
			fmt.Println("No qSync devices found. Make sure qSync server is started on the phone and connected to the same Wi-Fi.")
			return
		}

		fmt.Printf("✅ Found %d device(s):\n\n", len(devices))
		for i, d := range devices {
			fmt.Printf(" [%d] %s\n", i+1, d.Name)
			fmt.Printf("     IP:   %s:%d\n", d.IP, d.Port)
			fmt.Printf("     Host: %s\n\n", d.Host)
		}
	},
}

var (
	pairIP   string
	pairPort int
	pairPIN  string
)

var pairCmd = &cobra.Command{
	Use:   "pair",
	Short: "Pair this computer with your phone using a 6-digit PIN",
	Run: func(cmd *cobra.Command, args []string) {
		targetIP := pairIP
		targetPort := pairPort

		if targetIP == "" {
			fmt.Println("🔍 Searching for phone on Wi-Fi...")
			devices, _ := discovery.Scan(2 * time.Second)
			if len(devices) > 0 {
				targetIP = devices[0].IP
				targetPort = devices[0].Port
				fmt.Printf("✅ Found phone '%s' at %s:%d\n", devices[0].Name, targetIP, targetPort)
			} else {
				fmt.Print("Phone not auto-detected. Enter IP address shown on phone (e.g. 192.168.3.40): ")
				reader := bufio.NewReader(os.Stdin)
				input, _ := reader.ReadString('\n')
				targetIP = strings.TrimSpace(input)
				if targetIP == "" {
					fmt.Println("Cancelled.")
					return
				}
				if targetPort == 0 {
					targetPort = 8484
				}
			}
		}

		pin := pairPIN
		if pin == "" {
			fmt.Print("Enter the 6-digit PIN shown on phone: ")
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			pin = strings.TrimSpace(input)
		}

		if len(pin) != 6 {
			fmt.Println("❌ Invalid PIN: must be exactly 6 digits.")
			return
		}

		fmt.Println("🤝 Pairing...")
		token, serverName, err := client.Pair(targetIP, targetPort, pin, "")
		if err != nil {
			fmt.Printf("❌ Pairing failed: %v\n", err)
			return
		}

		if err := keyring.SaveToken("", token); err != nil {
			fmt.Printf("❌ Failed to store token in Keyring: %v\n", err)
			return
		}

		_ = keyring.SaveConfig(keyring.Config{
			LastServerName: serverName,
			LastServerIP:   targetIP,
			LastServerPort: targetPort,
		})

		fmt.Printf("🎉 Successfully paired with '%s'!\n", serverName)
		fmt.Println("Token securely saved in OS Keyring.")
		fmt.Println("You can now run 'qsync mount' to access the phone.")
	},
}

var openAfterMount bool

var mountCmd = &cobra.Command{
	Use:   "mount",
	Short: "Mount phone drive in file manager (Nautilus)",
	Run: func(cmd *cobra.Command, args []string) {
		token, err := keyring.GetToken("")
		if err != nil || token == "" {
			fmt.Println("❌ Device not paired yet. Please run 'qsync pair' first.")
			return
		}

		cfg, err := keyring.LoadConfig()
		targetIP := cfg.LastServerIP
		targetPort := cfg.LastServerPort

		// Verify or discover IP
		if targetIP == "" || !client.Ping(targetIP, targetPort) {
			fmt.Println("🔍 Searching for phone on network...")
			devices, err := discovery.Scan(2 * time.Second)
			if err == nil && len(devices) > 0 {
				targetIP = devices[0].IP
				targetPort = devices[0].Port
				cfg.LastServerIP = targetIP
				cfg.LastServerPort = targetPort
				_ = keyring.SaveConfig(cfg)
			}
		}

		if targetIP == "" {
			fmt.Println("❌ Phone not found on local Wi-Fi. Ensure qSync server is running.")
			return
		}

		m := mount.GetMounter()
		isMounted, _ := m.IsMounted(targetIP, targetPort)
		if isMounted {
			fmt.Printf("Phone is already mounted at %s:%d\n", targetIP, targetPort)
		} else {
			fmt.Printf("📁 Mounting %s:%d...\n", targetIP, targetPort)
			if err := m.Mount(targetIP, targetPort, token); err != nil {
				fmt.Printf("❌ Mount error: %v\n", err)
				return
			}
			fmt.Println("✅ Successfully mounted! Accessible in Nautilus.")
		}

		if openAfterMount {
			_ = m.OpenInFileManager(targetIP, targetPort)
		}
	},
}

var unmountCmd = &cobra.Command{
	Use:   "unmount",
	Short: "Unmount phone drive",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, _ := keyring.LoadConfig()
		m := mount.GetMounter()

		if cfg.LastServerIP == "" {
			fmt.Println("❌ No known phone recorded. Run 'qsync mount' first.")
			return
		}

		fmt.Println("Unmounting...")
		if err := m.Unmount(cfg.LastServerIP, cfg.LastServerPort); err != nil {
			fmt.Printf("Notice: %v\n", err)
		} else {
			fmt.Println("✅ Unmounted successfully.")
		}
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check connection and mount status",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, _ := keyring.LoadConfig()
		token, _ := keyring.GetToken("")

		fmt.Println("qSync Status:")
		fmt.Printf("  Pairing: ")
		if token != "" {
			fmt.Println("✅ Paired (Token stored in Keyring)")
		} else {
			fmt.Println("❌ Not paired (Run 'qsync pair')")
		}

		fmt.Printf("  Known Phone: %s (%s:%d)\n", cfg.LastServerName, cfg.LastServerIP, cfg.LastServerPort)

		isReachable := false
		if cfg.LastServerIP != "" {
			isReachable = client.Ping(cfg.LastServerIP, cfg.LastServerPort)
		}
		fmt.Printf("  Network: ")
		if isReachable {
			fmt.Println("🟢 Online (Reachable on Wi-Fi)")
		} else {
			fmt.Println("🔴 Offline or not detected")
		}

		m := mount.GetMounter()
		isMounted := false
		if cfg.LastServerIP != "" {
			isMounted, _ = m.IsMounted(cfg.LastServerIP, cfg.LastServerPort)
		}
		fmt.Printf("  Mount:   ")
		if isMounted {
			fmt.Println("📂 Mounted in GVFS / Nautilus")
		} else {
			fmt.Println("Unmounted")
		}
	},
}

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Run background D-Bus service for GNOME Shell extension",
	Run: func(cmd *cobra.Command, args []string) {
		conn, err := godbus.ConnectSessionBus()
		if err != nil {
			fmt.Printf("Failed to connect to session bus: %v\n", err)
			return
		}
		defer conn.Close()

		reply, err := conn.RequestName("org.qsync.Daemon", godbus.NameFlagDoNotQueue)
		if err != nil {
			fmt.Printf("Failed to request D-Bus name: %v\n", err)
			return
		}
		if reply != godbus.RequestNameReplyPrimaryOwner {
			fmt.Println("Another instance of qsync daemon is already running.")
			return
		}

		service := dbus.NewDaemonService(conn)
		if err := conn.Export(service, dbus.ObjectPath, dbus.InterfaceName); err != nil {
			fmt.Printf("Failed to export D-Bus service: %v\n", err)
			return
		}

		service.StartMonitor()
		fmt.Println("🚀 qSync D-Bus daemon running on org.qsync.Daemon...")

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		fmt.Println("\nStopping daemon...")
		service.Stop()
	},
}

func init() {
	pairCmd.Flags().StringVar(&pairIP, "ip", "", "Phone IP address")
	pairCmd.Flags().IntVar(&pairPort, "port", 8484, "Phone port")
	pairCmd.Flags().StringVar(&pairPIN, "pin", "", "6-digit pairing PIN")

	mountCmd.Flags().BoolVarP(&openAfterMount, "open", "o", false, "Open in file manager after mounting")

	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(pairCmd)
	rootCmd.AddCommand(mountCmd)
	rootCmd.AddCommand(unmountCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(daemonCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

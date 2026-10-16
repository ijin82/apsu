package main

import (
	"bufio"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"github.com/spf13/cobra"

	"apsu/cli/internal/client"
	"apsu/cli/internal/dbus"
	"apsu/cli/internal/discovery"
	"apsu/cli/internal/keyring"
	"apsu/cli/internal/mount"
	apsusync "apsu/cli/internal/sync"
)

var rootCmd = &cobra.Command{
	Use:   "apsu",
	Short: "Apsu - Android Phone Sync Utility for Linux GNOME",
}

var scanCmd = &cobra.Command{
	Use:   "scan",
	Short: "Scan local network for active Apsu phones",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("🔍 Scanning local network for Apsu devices (3s)...")
		devices, err := discovery.Scan(3 * time.Second)
		if err != nil {
			fmt.Printf("❌ Scan error: %v\n", err)
			return
		}
		if len(devices) == 0 {
			fmt.Println("No Apsu devices found. Make sure Apsu server is started on the phone and connected to the same Wi-Fi.")
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
		token, serverName, tlsPort, certFP, err := client.PairSecure(targetIP, targetPort, pin, "")
		if err != nil {
			fmt.Printf("❌ Pairing failed: %v\n", err)
			return
		}

		_ = keyring.UpdatePairedDeviceSecure(serverName, targetIP, targetPort, tlsPort, certFP, token)

		fmt.Printf("🎉 Successfully paired with '%s'!\n", serverName)
		fmt.Println("Token saved in configuration.")
		fmt.Println("You can now run 'apsu mount' to access the phone.")
	},
}

var openAfterMount bool

var mountCmd = &cobra.Command{
	Use:   "mount [device_name]",
	Short: "Mount phone drive in file manager (Nautilus)",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, _ := keyring.LoadConfig()

		targetName := ""
		if len(args) > 0 {
			targetName = args[0]
		}

		targetIP := cfg.LastServerIP
		targetPort := cfg.LastServerPort

		if targetName != "" {
			if dev, ok := cfg.PairedDevices[targetName]; ok {
				targetIP = dev.IP
				targetPort = dev.Port
			} else {
				// Search by prefix or case-insensitive
				for name, dev := range cfg.PairedDevices {
					if strings.EqualFold(name, targetName) {
						targetName = name
						targetIP = dev.IP
						targetPort = dev.Port
						break
					}
				}
			}
		}

		// Verify or discover IP if missing or unreachable
		if targetIP == "" || !client.Ping(targetIP, targetPort) {
			fmt.Println("🔍 Searching for devices on network...")
			devices, err := discovery.Scan(2 * time.Second)
			if err == nil && len(devices) > 0 {
				var matched *discovery.DiscoveredDevice
				if targetName != "" {
					for _, d := range devices {
						if strings.EqualFold(d.Name, targetName) {
							matched = &d
							break
						}
					}
				} else {
					matched = &devices[0]
				}

				if matched != nil {
					targetIP = matched.IP
					targetPort = matched.Port
					targetName = matched.Name
					cfg.LastServerName = targetName
					cfg.LastServerIP = targetIP
					cfg.LastServerPort = targetPort
					_ = keyring.SaveConfig(cfg)
				}
			}
		}

		if targetIP == "" {
			fmt.Println("❌ Phone not found on local Wi-Fi. Ensure Apsu server is running.")
			return
		}

		deviceKey := keyring.NormalizeServerKey(targetName)
		token, err := keyring.GetToken(deviceKey)
		if err != nil || token == "" {
			token, err = keyring.GetToken("")
		}
		if err != nil || token == "" {
			fmt.Println("❌ Device not paired yet. Please run 'apsu pair' first.")
			return
		}

		if !client.VerifyToken(targetIP, targetPort, token) {
			fmt.Println("❌ Device rejected the saved token (pairing mismatch or app reinstalled on phone).")
			fmt.Println("👉 Please run 'apsu pair' to pair with the phone again.")
			return
		}

		_ = keyring.UpdatePairedDevice(targetName, targetIP, targetPort, token)

		m := mount.GetMounter()
		isMounted, _ := m.IsMounted(targetIP, targetPort)
		if isMounted {
			fmt.Printf("Phone '%s' is already mounted at %s:%d\n", targetName, targetIP, targetPort)
		} else {
			fmt.Printf("📁 Mounting %s (%s:%d)...\n", targetName, targetIP, targetPort)
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
	Use:   "unmount [device_name]",
	Short: "Unmount phone drive",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, _ := keyring.LoadConfig()
		m := mount.GetMounter()

		targetIP := cfg.LastServerIP
		targetPort := cfg.LastServerPort

		if len(args) > 0 {
			targetName := args[0]
			if dev, ok := cfg.PairedDevices[targetName]; ok {
				targetIP = dev.IP
				targetPort = dev.Port
			} else {
				for name, dev := range cfg.PairedDevices {
					if strings.EqualFold(name, targetName) {
						targetIP = dev.IP
						targetPort = dev.Port
						break
					}
				}
			}
		}

		if targetIP == "" {
			fmt.Println("❌ No known phone recorded. Run 'apsu mount' first.")
			return
		}

		fmt.Printf("Unmounting %s:%d...\n", targetIP, targetPort)
		if err := m.Unmount(targetIP, targetPort); err != nil {
			fmt.Printf("Notice: %v\n", err)
		} else {
			fmt.Println("✅ Unmounted successfully.")
		}
	},
}

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Check connection and mount status of all devices",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, _ := keyring.LoadConfig()
		m := mount.GetMounter()

		fmt.Println("Apsu Multi-Device Status:")

		if len(cfg.PairedDevices) == 0 && cfg.LastServerName == "" {
			fmt.Println("  No paired devices. Run 'apsu pair' to add a device.")
			return
		}

		// Ensure primary device is represented
		devicesToShow := make(map[string]keyring.DeviceInfo)
		for k, v := range cfg.PairedDevices {
			devicesToShow[k] = v
		}
		if cfg.LastServerName != "" {
			if _, exists := devicesToShow[cfg.LastServerName]; !exists {
				devicesToShow[cfg.LastServerName] = keyring.DeviceInfo{
					Name: cfg.LastServerName,
					IP:   cfg.LastServerIP,
					Port: cfg.LastServerPort,
				}
			}
		}

		for name, dev := range devicesToShow {
			deviceKey := keyring.NormalizeServerKey(name)
			token, _ := keyring.GetToken(deviceKey)
			if token == "" {
				token, _ = keyring.GetToken("")
			}

			isReachable := false
			if dev.IP != "" {
				isReachable = client.Ping(dev.IP, dev.Port)
				if isReachable {
					if actualName := client.FetchServerName(dev.IP, dev.Port); actualName != "" && actualName != name {
						name = actualName
					}
				}
			}

			isMounted := false
			if dev.IP != "" {
				isMounted, _ = m.IsMounted(dev.IP, dev.Port)
			}

			fmt.Printf("\n📱 Device: %s (%s:%d)\n", name, dev.IP, dev.Port)
			if token != "" && isReachable {
				if client.VerifyToken(dev.IP, dev.Port, token) {
					fmt.Println("   Pairing:  ✅ Paired")
					_ = keyring.UpdatePairedDevice(name, dev.IP, dev.Port, token)
				} else {
					fmt.Println("   Pairing:  ⚠️ Invalid/Expired Token (re-pair required: 'apsu pair')")
				}
			} else if token != "" {
				fmt.Println("   Pairing:  ⚪ Paired (offline)")
			} else {
				fmt.Println("   Pairing:  ❌ Token not found (run 'apsu pair')")
			}

			if isReachable {
				fmt.Println("   Network:  🟢 Online")
			} else {
				fmt.Println("   Network:  🔴 Offline")
			}

			if isMounted {
				fmt.Println("   Mount:    📂 Mounted in GVFS / Nautilus")
			} else {
				fmt.Println("   Mount:    ⚪ Unmounted")
			}
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

		reply, err := conn.RequestName("org.apsu.Daemon", godbus.NameFlagDoNotQueue)
		if err != nil {
			fmt.Printf("Failed to request D-Bus name: %v\n", err)
			return
		}
		if reply != godbus.RequestNameReplyPrimaryOwner {
			fmt.Println("Another instance of apsu daemon is already running.")
			return
		}

		service := dbus.NewDaemonService(conn)
		if err := conn.Export(service, dbus.ObjectPath, dbus.InterfaceName); err != nil {
			fmt.Printf("Failed to export D-Bus service: %v\n", err)
			return
		}

		service.StartMonitor()
		fmt.Println("🚀 Apsu D-Bus daemon running on org.apsu.Daemon...")

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan

		fmt.Println("\nStopping daemon...")
		service.Stop()
	},
}

var (
	syncList bool
	syncMap  string
)

var syncCmd = &cobra.Command{
	Use:   "sync [device_name]",
	Short: "Synchronize desktop folders to phone (Desktop ➔ Phone One-Way Sync)",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, _ := keyring.LoadConfig()
		targetName := cfg.LastServerName
		if len(args) > 0 {
			targetName = args[0]
		}
		if targetName == "" {
			for name := range cfg.PairedDevices {
				targetName = name
				break
			}
		}

		if targetName == "" {
			fmt.Println("❌ No device specified and no paired devices found. Run 'apsu pair' first.")
			return
		}

		// Handle --map flag: e.g. --map "Music=/home/user/Music"
		if syncMap != "" {
			parts := strings.SplitN(syncMap, "=", 2)
			if len(parts) != 2 {
				fmt.Println("❌ Invalid --map format. Use: --map \"RemoteFolder=/path/to/local\"")
				return
			}
			remoteFolder := strings.TrimSpace(parts[0])
			localPath := strings.TrimSpace(parts[1])
			absPath, err := filepath.Abs(localPath)
			if err == nil {
				localPath = absPath
			}
			if err := keyring.SetSyncMapping(targetName, remoteFolder, localPath); err != nil {
				fmt.Printf("❌ Failed to save mapping: %v\n", err)
				return
			}
			fmt.Printf("✅ Mapped remote folder '%s' to '%s' for device '%s'\n", remoteFolder, localPath, targetName)
			return
		}

		// Handle --list flag
		if syncList {
			mappings := keyring.GetSyncMappings(targetName)
			fmt.Printf("Sync Folder Mappings for '%s':\n", targetName)
			if len(mappings) == 0 {
				fmt.Println("  (No mappings configured yet. Run 'apsu sync' while device is online to discover folders)")
				return
			}
			for rFolder, lPath := range mappings {
				status := "configured"
				if lPath == "" {
					status = "unmapped (edit in ~/.config/apsu/client_config.json)"
				} else if _, err := os.Stat(lPath); err != nil {
					status = "local path not found"
				}
				fmt.Printf("  • %s -> %s [%s]\n", rFolder, lPath, status)
			}
			return
		}

		// Find device IP and port
		targetIP := cfg.LastServerIP
		targetPort := cfg.LastServerPort
		if dev, ok := cfg.PairedDevices[targetName]; ok {
			targetIP = dev.IP
			targetPort = dev.Port
		} else {
			for name, dev := range cfg.PairedDevices {
				if strings.EqualFold(name, targetName) {
					targetIP = dev.IP
					targetPort = dev.Port
					targetName = name
					break
				}
			}
		}

		if targetIP == "" {
			devices, _ := discovery.Scan(2 * time.Second)
			for _, dev := range devices {
				if strings.EqualFold(dev.Name, targetName) {
					targetIP = dev.IP
					targetPort = dev.Port
					break
				}
			}
		}

		if targetIP == "" {
			fmt.Printf("❌ Device '%s' not found on local network.\n", targetName)
			return
		}
		if targetPort <= 0 {
			targetPort = 8484
		}

		deviceKey := keyring.NormalizeServerKey(targetName)
		token, err := keyring.GetToken(deviceKey)
		if err != nil || token == "" {
			token, _ = keyring.GetToken("")
		}
		if token == "" {
			fmt.Printf("❌ Device '%s' not paired yet. Please run 'apsu pair' first.\n", targetName)
			return
		}

		if !client.VerifyToken(targetIP, targetPort, token) {
			fmt.Println("❌ Device rejected saved token. Please run 'apsu pair' again.")
			return
		}

		logger, _ := apsusync.NewSyncLogger("")
		engine := apsusync.NewSyncEngine(logger)

		fmt.Printf("🔄 Synchronizing folders for '%s' (%s:%d)...\n", targetName, targetIP, targetPort)
		stats, summary, err := engine.SyncDevice(targetName, targetIP, targetPort, token)
		if err != nil {
			fmt.Printf("❌ Sync failed: %v\n", err)
			return
		}

		fmt.Printf("✅ Sync completed: %s\n", summary)
		if stats.ErrorCount > 0 {
			fmt.Printf("⚠️ %d errors encountered. Check ~/.config/apsu/sync_history.log for details.\n", stats.ErrorCount)
		} else {
			fmt.Println("📁 All folders up to date! Log saved to ~/.config/apsu/sync_history.log")
		}
	},
}

func init() {
	pairCmd.Flags().StringVar(&pairIP, "ip", "", "Phone IP address")
	pairCmd.Flags().IntVar(&pairPort, "port", 8484, "Phone port")
	pairCmd.Flags().StringVar(&pairPIN, "pin", "", "6-digit pairing PIN")

	mountCmd.Flags().BoolVarP(&openAfterMount, "open", "o", false, "Open in file manager after mounting")

	syncCmd.Flags().BoolVar(&syncList, "list", false, "List configured sync folders and their mappings")
	syncCmd.Flags().StringVar(&syncMap, "map", "", "Set mapping: \"RemoteFolder=/path/to/local\"")

	rootCmd.AddCommand(scanCmd)
	rootCmd.AddCommand(pairCmd)
	rootCmd.AddCommand(mountCmd)
	rootCmd.AddCommand(unmountCmd)
	rootCmd.AddCommand(statusCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(daemonCmd)
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

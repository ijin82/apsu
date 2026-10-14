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

		_ = keyring.SaveToken("", token)
		deviceKey := keyring.NormalizeServerKey(serverName)
		_ = keyring.SaveToken(deviceKey, token)

		cfg, _ := keyring.LoadConfig()
		cfg.LastServerName = serverName
		cfg.LastServerIP = targetIP
		cfg.LastServerPort = targetPort
		if cfg.PairedDevices == nil {
			cfg.PairedDevices = make(map[string]keyring.DeviceInfo)
		}
		cfg.PairedDevices[serverName] = keyring.DeviceInfo{
			Name:  serverName,
			IP:    targetIP,
			Port:  targetPort,
			Token: token,
		}
		_ = keyring.SaveConfig(cfg)

		fmt.Printf("🎉 Successfully paired with '%s'!\n", serverName)
		fmt.Println("Token saved in configuration.")
		fmt.Println("You can now run 'qsync mount' to access the phone.")
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
			fmt.Println("❌ Phone not found on local Wi-Fi. Ensure qSync server is running.")
			return
		}

		deviceKey := keyring.NormalizeServerKey(targetName)
		token, err := keyring.GetToken(deviceKey)
		if err != nil || token == "" {
			token, err = keyring.GetToken("")
		}
		if err != nil || token == "" {
			fmt.Println("❌ Device not paired yet. Please run 'qsync pair' first.")
			return
		}

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
			fmt.Println("❌ No known phone recorded. Run 'qsync mount' first.")
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

		fmt.Println("qSync Multi-Device Status:")

		if len(cfg.PairedDevices) == 0 && cfg.LastServerName == "" {
			fmt.Println("  No paired devices. Run 'qsync pair' to add a device.")
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
			}

			isMounted := false
			if dev.IP != "" {
				isMounted, _ = m.IsMounted(dev.IP, dev.Port)
			}

			fmt.Printf("\n📱 Device: %s (%s:%d)\n", name, dev.IP, dev.Port)
			if token != "" {
				fmt.Println("   Pairing:  ✅ Paired")
			} else {
				fmt.Println("   Pairing:  ❌ Token not found")
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

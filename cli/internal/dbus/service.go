package dbus

import (
	"sync"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"qsync/cli/internal/client"
	"qsync/cli/internal/discovery"
	"qsync/cli/internal/keyring"
	"qsync/cli/internal/mount"
)

const (
	InterfaceName = "org.qsync.Daemon"
	ObjectPath    = "/org/qsync/Daemon"
)

type DaemonService struct {
	mu         sync.RWMutex
	conn       *godbus.Conn
	mounter    mount.Mounter
	phoneFound bool
	phoneName  string
	phoneIP    string
	phonePort  int
	isMounted  bool
	stopChan   chan struct{}
}

func NewDaemonService(conn *godbus.Conn) *DaemonService {
	return &DaemonService{
		conn:     conn,
		mounter:  mount.GetMounter(),
		stopChan: make(chan struct{}),
	}
}

func (d *DaemonService) GetStatus() (bool, string, string, int, bool, *godbus.Error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.phoneFound, d.phoneName, d.phoneIP, d.phonePort, d.isMounted, nil
}

func (d *DaemonService) Pair(pin string, ip string, port int) (bool, string, string, *godbus.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	targetIP := ip
	targetPort := port
	if targetIP == "" {
		if d.phoneIP != "" {
			targetIP = d.phoneIP
			targetPort = d.phonePort
		} else {
			cfg, _ := keyring.LoadConfig()
			if cfg.LastServerIP != "" && client.Ping(cfg.LastServerIP, cfg.LastServerPort) {
				targetIP = cfg.LastServerIP
				targetPort = cfg.LastServerPort
			} else {
				devices, _ := discovery.Scan(3 * time.Second)
				if len(devices) > 0 {
					targetIP = devices[0].IP
					targetPort = devices[0].Port
				}
			}
		}
	}
	if targetIP == "" {
		return false, "", "No phone found on Wi-Fi. Make sure qSync is running.", nil
	}
	if targetPort <= 0 {
		targetPort = 8484
	}

	token, serverName, err := client.Pair(targetIP, targetPort, pin, "")
	if err != nil {
		return false, "", err.Error(), nil
	}

	if err := keyring.SaveToken("", token); err != nil {
		return false, "", err.Error(), nil
	}

	_ = keyring.SaveConfig(keyring.Config{
		LastServerName: serverName,
		LastServerIP:   targetIP,
		LastServerPort: targetPort,
	})

	d.phoneFound = true
	d.phoneName = serverName
	d.phoneIP = targetIP
	d.phonePort = targetPort
	d.emitStatusChanged()

	return true, serverName, "", nil
}

func (d *DaemonService) Mount() (bool, string, *godbus.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if !d.phoneFound || d.phoneIP == "" {
		return false, "Phone not found in local network", nil
	}

	token, err := keyring.GetToken("")
	if err != nil || token == "" {
		return false, "Device not paired yet. Please run 'qsync pair' first.", nil
	}

	if err := d.mounter.Mount(d.phoneIP, d.phonePort, token); err != nil {
		return false, err.Error(), nil
	}

	d.isMounted = true
	d.emitStatusChanged()
	return true, "Mounted successfully", nil
}

func (d *DaemonService) Unmount() (bool, string, *godbus.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.phoneIP == "" {
		cfg, _ := keyring.LoadConfig()
		if cfg.LastServerIP != "" {
			d.phoneIP = cfg.LastServerIP
			d.phonePort = cfg.LastServerPort
		}
	}

	if d.phoneIP == "" {
		return false, "No known phone to unmount", nil
	}

	if err := d.mounter.Unmount(d.phoneIP, d.phonePort); err != nil {
		return false, err.Error(), nil
	}

	d.isMounted = false
	d.emitStatusChanged()
	return true, "Unmounted successfully", nil
}

func (d *DaemonService) OpenInNautilus() (bool, *godbus.Error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	if d.phoneIP == "" {
		return false, godbus.NewError("org.qsync.Error.NoPhone", []interface{}{"No phone known"})
	}

	err := d.mounter.OpenInFileManager(d.phoneIP, d.phonePort)
	if err != nil {
		return false, godbus.NewError("org.qsync.Error.OpenFailed", []interface{}{err.Error()})
	}
	return true, nil
}

func (d *DaemonService) emitStatusChanged() {
	if d.conn != nil {
		_ = d.conn.Emit(ObjectPath, InterfaceName+".StatusChanged", d.phoneFound, d.isMounted)
	}
}

func (d *DaemonService) StartMonitor() {
	go func() {
		ticker := time.NewTicker(3 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-d.stopChan:
				return
			case <-ticker.C:
				d.checkStatus()
			}
		}
	}()
}

func (d *DaemonService) checkStatus() {
	// First check if known phone is reachable via quick ping
	d.mu.RLock()
	currentIP := d.phoneIP
	currentPort := d.phonePort
	d.mu.RUnlock()

	var found bool
	var name, ip string
	var port int

	if currentIP != "" && client.Ping(currentIP, currentPort) {
		found = true
		ip = currentIP
		port = currentPort
	} else {
		// Discover via mDNS
		devices, err := discovery.Scan(1500 * time.Millisecond)
		if err == nil && len(devices) > 0 {
			found = true
			name = devices[0].Name
			ip = devices[0].IP
			port = devices[0].Port
		}
	}

	var isMounted bool
	if found {
		isMounted, _ = d.mounter.IsMounted(ip, port)
	}

	d.mu.Lock()
	changed := (d.phoneFound != found) || (d.isMounted != isMounted)
	d.phoneFound = found
	if name != "" {
		d.phoneName = name
	}
	if ip != "" {
		d.phoneIP = ip
		d.phonePort = port
	}
	d.isMounted = isMounted
	d.mu.Unlock()

	if changed {
		d.emitStatusChanged()
	}
}

func (d *DaemonService) Stop() {
	close(d.stopChan)
}

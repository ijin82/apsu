package dbus

import (
	"encoding/json"
	"strings"
	"sync"
	"time"

	godbus "github.com/godbus/dbus/v5"
	"qsync/cli/internal/client"
	"qsync/cli/internal/discovery"
	"qsync/cli/internal/keyring"
	"qsync/cli/internal/mount"
	qsyncsync "qsync/cli/internal/sync"
)

const (
	InterfaceName = "org.qsync.Daemon"
	ObjectPath    = "/org/qsync/Daemon"
)

type DeviceStatus struct {
	Name           string   `json:"name"`
	IP             string   `json:"ip"`
	Port           int      `json:"port"`
	IsOnline       bool     `json:"isOnline"`
	IsMounted      bool     `json:"isMounted"`
	IsPaired       bool     `json:"isPaired"`
	HasSyncFolders bool     `json:"hasSyncFolders"`
	SyncFolders    []string `json:"syncFolders,omitempty"`
	IsSyncing      bool     `json:"isSyncing"`
}

type DaemonService struct {
	mu              sync.RWMutex
	conn            *godbus.Conn
	mounter         mount.Mounter
	syncEngine      *qsyncsync.SyncEngine
	syncLogger      *qsyncsync.SyncLogger
	phoneFound      bool
	phoneName       string
	phoneIP         string
	phonePort       int
	isMounted       bool
	devices         map[string]DeviceStatus // key: IP
	syncingDevices  map[string]bool         // key: deviceName
	manualUnmounted map[string]bool         // key: IP, remembers manual unmount for the active session
	stopChan        chan struct{}
}

func NewDaemonService(conn *godbus.Conn) *DaemonService {
	logger, _ := qsyncsync.NewSyncLogger("")
	engine := qsyncsync.NewSyncEngine(logger)
	return &DaemonService{
		conn:            conn,
		mounter:         mount.GetMounter(),
		syncEngine:      engine,
		syncLogger:      logger,
		devices:         make(map[string]DeviceStatus),
		syncingDevices:  make(map[string]bool),
		manualUnmounted: make(map[string]bool),
		stopChan:        make(chan struct{}),
	}
}

// GetStatus returns the status of the primary/last active device for backward compatibility.
func (d *DaemonService) GetStatus() (bool, string, string, int, bool, *godbus.Error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.phoneFound, d.phoneName, d.phoneIP, d.phonePort, d.isMounted, nil
}

// GetDevicesJSON returns the complete list of discovered and paired devices as a JSON string.
func (d *DaemonService) GetDevicesJSON() (string, *godbus.Error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	list := make([]DeviceStatus, 0)
	for _, dev := range d.devices {
		list = append(list, dev)
	}

	bytes, err := json.Marshal(list)
	if err != nil {
		return "[]", nil
	}
	return string(bytes), nil
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

	token, serverName, tlsPort, certFP, err := client.PairSecure(targetIP, targetPort, pin, "")
	if err != nil {
		return false, "", err.Error(), nil
	}

	if err := keyring.UpdatePairedDeviceSecure(serverName, targetIP, targetPort, tlsPort, certFP, token); err != nil {
		return false, "", err.Error(), nil
	}

	d.phoneFound = true
	d.phoneName = serverName
	d.phoneIP = targetIP
	d.phonePort = targetPort

	d.devices[targetIP] = DeviceStatus{
		Name:      serverName,
		IP:        targetIP,
		Port:      targetPort,
		IsOnline:  true,
		IsMounted: false,
		IsPaired:  true,
	}

	d.emitStatusChanged()
	return true, serverName, "", nil
}

func (d *DaemonService) Mount() (bool, string, *godbus.Error) {
	d.mu.RLock()
	targetIP := d.phoneIP
	targetPort := d.phonePort
	targetName := d.phoneName
	d.mu.RUnlock()

	return d.MountDevice(targetName, targetIP, targetPort)
}

func (d *DaemonService) MountDevice(name string, ip string, port int) (bool, string, *godbus.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	targetIP := ip
	targetPort := port
	targetName := name

	// If IP is not provided, look up by name or fallback
	if targetIP == "" {
		for _, dev := range d.devices {
			if strings.EqualFold(dev.Name, targetName) {
				targetIP = dev.IP
				targetPort = dev.Port
				break
			}
		}
	}

	if targetIP == "" {
		targetIP = d.phoneIP
		targetPort = d.phonePort
		targetName = d.phoneName
	}

	if targetIP == "" {
		return false, "Device not found on local network", nil
	}
	if targetPort <= 0 {
		targetPort = 8484
	}

	// Try device-specific token first, fallback to default
	deviceKey := keyring.NormalizeServerKey(targetName)
	token, err := keyring.GetToken(deviceKey)
	if err != nil || token == "" {
		token, err = keyring.GetToken("")
	}
	if err != nil || token == "" {
		return false, "Device not paired yet. Please pair first.", nil
	}

	if !client.VerifyToken(targetIP, targetPort, token) {
		if dev, ok := d.devices[targetIP]; ok {
			dev.IsPaired = false
			d.devices[targetIP] = dev
		}
		d.emitStatusChanged()
		return false, "Device rejected token (app reinstalled or un-paired). Please pair again.", nil
	}

	if err := d.mounter.Mount(targetIP, targetPort, token); err != nil {
		if !client.VerifyToken(targetIP, targetPort, token) {
			if dev, ok := d.devices[targetIP]; ok {
				dev.IsPaired = false
				d.devices[targetIP] = dev
			}
			d.emitStatusChanged()
		}
		return false, err.Error(), nil
	}

	delete(d.manualUnmounted, targetIP)

	if dev, ok := d.devices[targetIP]; ok {
		dev.IsMounted = true
		d.devices[targetIP] = dev
	}
	if targetIP == d.phoneIP {
		d.isMounted = true
	}

	d.emitStatusChanged()
	return true, "Mounted successfully", nil
}

func (d *DaemonService) Unmount() (bool, string, *godbus.Error) {
	d.mu.RLock()
	targetIP := d.phoneIP
	targetPort := d.phonePort
	d.mu.RUnlock()

	return d.UnmountDevice("", targetIP, targetPort)
}

func (d *DaemonService) UnmountDevice(name string, ip string, port int) (bool, string, *godbus.Error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	targetIP := ip
	targetPort := port

	if targetIP == "" && name != "" {
		for _, dev := range d.devices {
			if strings.EqualFold(dev.Name, name) {
				targetIP = dev.IP
				targetPort = dev.Port
				break
			}
		}
	}

	if targetIP == "" {
		targetIP = d.phoneIP
		targetPort = d.phonePort
	}

	if targetIP == "" {
		return false, "No device specified to unmount", nil
	}
	if targetPort <= 0 {
		targetPort = 8484
	}

	if err := d.mounter.Unmount(targetIP, targetPort); err != nil {
		return false, err.Error(), nil
	}

	// Mark as manually unmounted so auto-mount won't re-mount it during this session
	d.manualUnmounted[targetIP] = true

	if dev, ok := d.devices[targetIP]; ok {
		dev.IsMounted = false
		d.devices[targetIP] = dev
	}
	if targetIP == d.phoneIP {
		d.isMounted = false
	}

	d.emitStatusChanged()
	return true, "Unmounted successfully", nil
}

func (d *DaemonService) OpenInNautilus() (bool, *godbus.Error) {
	d.mu.RLock()
	targetIP := d.phoneIP
	targetPort := d.phonePort
	d.mu.RUnlock()

	return d.OpenDeviceInFileManager("", targetIP, targetPort)
}

func (d *DaemonService) OpenDeviceInFileManager(name string, ip string, port int) (bool, *godbus.Error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	targetIP := ip
	targetPort := port

	if targetIP == "" && name != "" {
		for _, dev := range d.devices {
			if strings.EqualFold(dev.Name, name) {
				targetIP = dev.IP
				targetPort = dev.Port
				break
			}
		}
	}

	if targetIP == "" {
		targetIP = d.phoneIP
		targetPort = d.phonePort
	}

	if targetIP == "" {
		return false, godbus.NewError("org.qsync.Error.NoPhone", []interface{}{"No phone known"})
	}
	if targetPort <= 0 {
		targetPort = 8484
	}

	err := d.mounter.OpenInFileManager(targetIP, targetPort)
	if err != nil {
		return false, godbus.NewError("org.qsync.Error.OpenFailed", []interface{}{err.Error()})
	}
	return true, nil
}

type SyncFolderMappingDTO struct {
	Name      string `json:"name"`
	LocalPath string `json:"localPath"`
}

func (d *DaemonService) GetSyncFolders(deviceName, ip string, port int) (string, *godbus.Error) {
	d.mu.RLock()
	targetIP := ip
	targetPort := port
	targetName := deviceName
	if targetIP == "" && targetName != "" {
		for _, dev := range d.devices {
			if strings.EqualFold(dev.Name, targetName) {
				targetIP = dev.IP
				targetPort = dev.Port
				break
			}
		}
	}
	if targetIP == "" {
		targetIP = d.phoneIP
		targetPort = d.phonePort
		targetName = d.phoneName
	}
	d.mu.RUnlock()

	if targetIP == "" {
		return "[]", nil
	}
	if targetPort <= 0 {
		targetPort = 8484
	}

	info, err := client.FetchServerInfo(targetIP, targetPort)
	if err != nil || info == nil {
		return "[]", nil
	}

	mappings := keyring.GetSyncMappings(targetName)
	var list []SyncFolderMappingDTO
	var syncNames []string
	for _, f := range info.Folders {
		if f.IsSync {
			syncNames = append(syncNames, f.Name)
			list = append(list, SyncFolderMappingDTO{
				Name:      f.Name,
				LocalPath: mappings[f.Name],
			})
		}
	}

	_ = keyring.EnsureSyncFoldersInConfig(targetName, syncNames)

	bytes, err := json.Marshal(list)
	if err != nil {
		return "[]", nil
	}
	return string(bytes), nil
}

func (d *DaemonService) SetSyncFolderMapping(deviceName, remoteFolder, localPath string) (bool, *godbus.Error) {
	if deviceName == "" || remoteFolder == "" {
		return false, godbus.NewError("org.qsync.Error.InvalidArgs", []interface{}{"Device name and remote folder are required"})
	}
	err := keyring.SetSyncMapping(deviceName, remoteFolder, localPath)
	if err != nil {
		return false, godbus.NewError("org.qsync.Error.SaveFailed", []interface{}{err.Error()})
	}
	return true, nil
}

func (d *DaemonService) SetSyncFolderMappings(deviceName, jsonMappings string) (bool, *godbus.Error) {
	if deviceName == "" || jsonMappings == "" {
		return false, godbus.NewError("org.qsync.Error.InvalidArgs", []interface{}{"Device name and jsonMappings are required"})
	}
	var mappings map[string]string
	if err := json.Unmarshal([]byte(jsonMappings), &mappings); err != nil {
		return false, godbus.NewError("org.qsync.Error.InvalidJSON", []interface{}{err.Error()})
	}
	if err := keyring.SetSyncMappingsBatch(deviceName, mappings); err != nil {
		return false, godbus.NewError("org.qsync.Error.SaveFailed", []interface{}{err.Error()})
	}
	return true, nil
}

func (d *DaemonService) SyncDevice(deviceName, ip string, port int) (bool, string, string, *godbus.Error) {
	d.mu.Lock()
	targetIP := ip
	targetPort := port
	targetName := deviceName
	if targetIP == "" && targetName != "" {
		for _, dev := range d.devices {
			if strings.EqualFold(dev.Name, targetName) {
				targetIP = dev.IP
				targetPort = dev.Port
				break
			}
		}
	}
	if targetIP == "" {
		targetIP = d.phoneIP
		targetPort = d.phonePort
		targetName = d.phoneName
	}

	if targetIP == "" {
		d.mu.Unlock()
		return false, "", "No device found to sync", nil
	}
	if targetPort <= 0 {
		targetPort = 8484
	}

	// Prevent duplicate concurrent sync runs for the same device
	if d.syncingDevices[targetName] {
		d.mu.Unlock()
		return false, "", "Sync already in progress for this device", nil
	}

	d.syncingDevices[targetName] = true
	if dev, ok := d.devices[targetIP]; ok {
		dev.IsSyncing = true
		d.devices[targetIP] = dev
	}
	d.mu.Unlock()

	deviceKey := keyring.NormalizeServerKey(targetName)
	token, err := keyring.GetToken(deviceKey)
	if err != nil || token == "" {
		token, _ = keyring.GetToken("")
	}
	if token == "" {
		d.mu.Lock()
		delete(d.syncingDevices, targetName)
		if dev, ok := d.devices[targetIP]; ok {
			dev.IsSyncing = false
			d.devices[targetIP] = dev
		}
		d.mu.Unlock()
		return false, "", "Device is not paired yet. Please pair first.", nil
	}

	d.emitSyncStarted(targetName)
	d.emitStatusChanged()

	// Run sync in background goroutine to avoid blocking D-Bus calls or timing out
	go func() {
		defer func() {
			d.mu.Lock()
			delete(d.syncingDevices, targetName)
			if dev, ok := d.devices[targetIP]; ok {
				dev.IsSyncing = false
				d.devices[targetIP] = dev
			}
			d.mu.Unlock()
			d.emitStatusChanged()
		}()

		stats, summary, err := d.syncEngine.SyncDevice(targetName, targetIP, targetPort, token)
		if err != nil {
			d.emitSyncCompleted(targetName, false, err.Error())
			return
		}

		success := stats.ErrorCount == 0
		d.emitSyncCompleted(targetName, success, summary)
	}()

	return true, "Sync started", "", nil
}

func (d *DaemonService) emitSyncStarted(deviceName string) {
	if d.conn != nil {
		_ = d.conn.Emit(ObjectPath, InterfaceName+".SyncStarted", deviceName)
	}
}

func (d *DaemonService) emitSyncCompleted(deviceName string, success bool, message string) {
	if d.conn != nil {
		_ = d.conn.Emit(ObjectPath, InterfaceName+".SyncCompleted", deviceName, success, message)
	}
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
	cfg, _ := keyring.LoadConfig()

	// Discover devices via mDNS
	discovered, err := discovery.Scan(1500 * time.Millisecond)
	if err != nil {
		discovered = nil
	}

	newDevices := make(map[string]DeviceStatus)

	// Add discovered devices
	for _, disc := range discovered {
		isMounted, _ := d.mounter.IsMounted(disc.IP, disc.Port)
		name := disc.Name
		if actualName := client.FetchServerName(disc.IP, disc.Port); actualName != "" {
			name = actualName
		}

		// Look up token strictly for known paired devices (do not leak DefaultToken to unknown devices)
		token := ""
		if p, ok := cfg.PairedDevices[name]; ok && p.Token != "" {
			token = p.Token
		} else if p, ok := cfg.PairedDevices[disc.Name]; ok && p.Token != "" {
			token = p.Token
		} else {
			for _, p := range cfg.PairedDevices {
				if p.IP == disc.IP && p.Token != "" {
					token = p.Token
					break
				}
			}
		}
		if token == "" {
			deviceKey := keyring.NormalizeServerKey(name)
			if t, err := keyring.GetToken(deviceKey); err == nil && t != "" {
				token = t
			} else if disc.Name != name {
				if t, err := keyring.GetToken(keyring.NormalizeServerKey(disc.Name)); err == nil && t != "" {
					token = t
				}
			}
		}

		isPaired := false
		if isMounted {
			isPaired = true
		} else if token != "" {
			if client.VerifyToken(disc.IP, disc.Port, token) {
				isPaired = true
				// Requirement 2: Token matched! Sync device name in config if changed
				_ = keyring.UpdatePairedDevice(name, disc.IP, disc.Port, token)
			}
		}

		var hasSyncFolders bool
		var syncFolderNames []string
		if isPaired {
			if info, err := client.FetchServerInfo(disc.IP, disc.Port); err == nil && info != nil {
				for _, f := range info.Folders {
					if f.IsSync {
						hasSyncFolders = true
						syncFolderNames = append(syncFolderNames, f.Name)
					}
				}
				if hasSyncFolders {
					_ = keyring.EnsureSyncFoldersInConfig(name, syncFolderNames)
				}
			}
		}

		d.mu.RLock()
		isSyncing := d.syncingDevices[name]
		d.mu.RUnlock()

		newDevices[disc.IP] = DeviceStatus{
			Name:           name,
			IP:             disc.IP,
			Port:           disc.Port,
			IsOnline:       true,
			IsMounted:      isMounted,
			IsPaired:       isPaired,
			HasSyncFolders: hasSyncFolders,
			SyncFolders:    syncFolderNames,
			IsSyncing:      isSyncing,
		}
	}

	// Add known paired devices from config if not yet in newDevices
	for _, p := range cfg.PairedDevices {
		if _, exists := newDevices[p.IP]; !exists {
			online := client.Ping(p.IP, p.Port)
			isMounted, _ := d.mounter.IsMounted(p.IP, p.Port)
			name := p.Name
			if online {
				if actualName := client.FetchServerName(p.IP, p.Port); actualName != "" {
					name = actualName
				}
			}

			token := p.Token
			if token == "" {
				deviceKey := keyring.NormalizeServerKey(name)
				token, _ = keyring.GetToken(deviceKey)
			}

			isPaired := false
			if isMounted {
				isPaired = true
			} else if online {
				if token != "" && client.VerifyToken(p.IP, p.Port, token) {
					isPaired = true
					// Requirement 2: Token matched! Sync device name in config if changed
					_ = keyring.UpdatePairedDevice(name, p.IP, p.Port, token)
				}
			} else {
				// Offline device retains pairing flag from config
				isPaired = token != ""
			}

			var hasSyncFolders bool
			var syncFolderNames []string
			if isPaired && online {
				if info, err := client.FetchServerInfo(p.IP, p.Port); err == nil && info != nil {
					for _, f := range info.Folders {
						if f.IsSync {
							hasSyncFolders = true
							syncFolderNames = append(syncFolderNames, f.Name)
						}
					}
					if hasSyncFolders {
						_ = keyring.EnsureSyncFoldersInConfig(name, syncFolderNames)
					}
				}
			}

			d.mu.RLock()
			isSyncing := d.syncingDevices[name]
			d.mu.RUnlock()

			newDevices[p.IP] = DeviceStatus{
				Name:           name,
				IP:             p.IP,
				Port:           p.Port,
				IsOnline:       online,
				IsMounted:      isMounted,
				IsPaired:       isPaired,
				HasSyncFolders: hasSyncFolders,
				SyncFolders:    syncFolderNames,
				IsSyncing:      isSyncing,
			}
		}
	}

	// Auto-mount paired devices that come online; auto-unmount devices that went offline
	d.mu.Lock()
	for ip, dev := range newDevices {
		if dev.IsOnline {
			// If device was mounted and is still mounted, clear manualUnmounted
			if dev.IsMounted {
				delete(d.manualUnmounted, ip)
			} else if dev.IsPaired && !d.manualUnmounted[ip] {
				// Device is online, paired, not mounted, and not manually unmounted in this session -> Auto-mount
				deviceKey := keyring.NormalizeServerKey(dev.Name)
				token, _ := keyring.GetToken(deviceKey)
				if token == "" {
					token, _ = keyring.GetToken("")
				}
				if token != "" {
					if err := d.mounter.Mount(dev.IP, dev.Port, token); err == nil {
						dev.IsMounted = true
						newDevices[ip] = dev
						delete(d.manualUnmounted, ip)
					}
				}
			}
		} else {
			// Device went offline -> Clean up manualUnmounted flag so next time it joins, it auto-mounts freshly
			delete(d.manualUnmounted, ip)

			if dev.IsMounted {
				// Double check with ping before unmounting
				if !client.Ping(dev.IP, dev.Port) {
					_ = d.mounter.Unmount(dev.IP, dev.Port)
					dev.IsMounted = false
					newDevices[ip] = dev
				}
			}
		}
	}
	d.mu.Unlock()

	// Determine primary phone for backwards compatibility
	var primaryFound bool
	var primaryName, primaryIP string
	var primaryPort int
	var primaryMounted bool

	for _, dev := range newDevices {
		if dev.IsOnline {
			primaryFound = true
			primaryName = dev.Name
			primaryIP = dev.IP
			primaryPort = dev.Port
			primaryMounted = dev.IsMounted
			if dev.IsMounted {
				break
			}
		}
	}

	d.mu.Lock()
	d.devices = newDevices
	changed := (d.phoneFound != primaryFound) || (d.isMounted != primaryMounted) || (d.phoneIP != primaryIP)
	d.phoneFound = primaryFound
	if primaryName != "" {
		d.phoneName = primaryName
	}
	if primaryIP != "" {
		d.phoneIP = primaryIP
		d.phonePort = primaryPort
	}
	d.isMounted = primaryMounted
	d.mu.Unlock()

	if changed {
		d.emitStatusChanged()
	}
}

func (d *DaemonService) Stop() {
	close(d.stopChan)

	// Unmount all currently mounted devices on daemon shutdown
	d.mu.RLock()
	var mountedIPs []struct {
		ip   string
		port int
	}
	for _, dev := range d.devices {
		if dev.IsMounted {
			port := dev.Port
			if port <= 0 {
				port = 8484
			}
			mountedIPs = append(mountedIPs, struct {
				ip   string
				port int
			}{ip: dev.IP, port: port})
		}
	}
	d.mu.RUnlock()

	for _, m := range mountedIPs {
		_ = d.mounter.Unmount(m.ip, m.port)
	}
}


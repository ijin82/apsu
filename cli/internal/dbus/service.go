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
)

const (
	InterfaceName = "org.qsync.Daemon"
	ObjectPath    = "/org/qsync/Daemon"
)

type DeviceStatus struct {
	Name      string `json:"name"`
	IP        string `json:"ip"`
	Port      int    `json:"port"`
	IsOnline  bool   `json:"isOnline"`
	IsMounted bool   `json:"isMounted"`
	IsPaired  bool   `json:"isPaired"`
}

type DaemonService struct {
	mu             sync.RWMutex
	conn           *godbus.Conn
	mounter        mount.Mounter
	phoneFound     bool
	phoneName      string
	phoneIP        string
	phonePort      int
	isMounted      bool
	devices        map[string]DeviceStatus // key: IP
	stopChan       chan struct{}
}

func NewDaemonService(conn *godbus.Conn) *DaemonService {
	return &DaemonService{
		conn:     conn,
		mounter:  mount.GetMounter(),
		devices:  make(map[string]DeviceStatus),
		stopChan: make(chan struct{}),
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

	token, serverName, err := client.Pair(targetIP, targetPort, pin, "")
	if err != nil {
		return false, "", err.Error(), nil
	}

	// Save token both globally and device-specific
	_ = keyring.SaveToken("", token)
	deviceKey := keyring.NormalizeServerKey(serverName)
	if err := keyring.SaveToken(deviceKey, token); err != nil {
		return false, "", err.Error(), nil
	}

	cfg, _ := keyring.LoadConfig()
	cfg.LastServerName = serverName
	cfg.LastServerIP = targetIP
	cfg.LastServerPort = targetPort
	if cfg.PairedDevices == nil {
		cfg.PairedDevices = make(map[string]keyring.DeviceInfo)
	}
	cfg.PairedDevices[serverName] = keyring.DeviceInfo{
		Name: serverName,
		IP:   targetIP,
		Port: targetPort,
	}
	_ = keyring.SaveConfig(cfg)

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

	if err := d.mounter.Mount(targetIP, targetPort, token); err != nil {
		return false, err.Error(), nil
	}

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
		deviceKey := keyring.NormalizeServerKey(disc.Name)
		token, _ := keyring.GetToken(deviceKey)
		if token == "" {
			token, _ = keyring.GetToken("")
		}

		newDevices[disc.IP] = DeviceStatus{
			Name:      disc.Name,
			IP:        disc.IP,
			Port:      disc.Port,
			IsOnline:  true,
			IsMounted: isMounted,
			IsPaired:  token != "",
		}
	}

	// Add known paired devices from config if not yet in newDevices
	for _, p := range cfg.PairedDevices {
		if _, exists := newDevices[p.IP]; !exists {
			online := client.Ping(p.IP, p.Port)
			isMounted := false
			if online {
				isMounted, _ = d.mounter.IsMounted(p.IP, p.Port)
			}
			newDevices[p.IP] = DeviceStatus{
				Name:      p.Name,
				IP:        p.IP,
				Port:      p.Port,
				IsOnline:  online,
				IsMounted: isMounted,
				IsPaired:  true,
			}
		}
	}

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
}


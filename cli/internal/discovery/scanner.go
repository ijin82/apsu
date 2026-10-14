package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/grandcat/zeroconf"
)

type DiscoveredDevice struct {
	Name string            `json:"name"`
	Host string            `json:"host"`
	IP   string            `json:"ip"`
	Port int               `json:"port"`
	Txt  map[string]string `json:"txt"`
}

// Scan performs parallel mDNS and subnet discovery, returning immediately upon finding devices
func Scan(timeout time.Duration) ([]DiscoveredDevice, error) {
	deviceFound := make(chan DiscoveredDevice, 32)
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	var wg sync.WaitGroup

	// 1. mDNS Browse
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanMDNS(ctx, deviceFound)
	}()

	// 2. Fast Subnet Scan
	wg.Add(1)
	go func() {
		defer wg.Done()
		scanSubnets(ctx, 8484, deviceFound)
	}()

	// Wait in background and close channel
	go func() {
		wg.Wait()
		close(deviceFound)
	}()

	var allDevices []DiscoveredDevice
	seenIPs := make(map[string]bool)

	// Collect until timeout or at least one device found with small grace period
	graceTimer := time.NewTimer(timeout)
	defer graceTimer.Stop()

	for {
		select {
		case dev, ok := <-deviceFound:
			if !ok {
				return allDevices, nil
			}
			if dev.IP != "" && !seenIPs[dev.IP] {
				seenIPs[dev.IP] = true
				allDevices = append(allDevices, dev)
				// Device found! Return early after small delay to catch multi-device
				go func() {
					time.Sleep(300 * time.Millisecond)
					cancel()
				}()
			}
		case <-graceTimer.C:
			return allDevices, nil
		case <-ctx.Done():
			// Drain any remaining in channel quickly
			for dev := range deviceFound {
				if dev.IP != "" && !seenIPs[dev.IP] {
					seenIPs[dev.IP] = true
					allDevices = append(allDevices, dev)
				}
			}
			return allDevices, nil
		}
	}
}

func scanMDNS(ctx context.Context, out chan<- DiscoveredDevice) {
	resolver, err := zeroconf.NewResolver(nil)
	if err != nil {
		return
	}

	entries := make(chan *zeroconf.ServiceEntry)
	go func() {
		for entry := range entries {
			dev := DiscoveredDevice{
				Name: entry.Instance,
				Host: entry.HostName,
				Port: entry.Port,
				Txt:  make(map[string]string),
			}
			for _, t := range entry.Text {
				parts := strings.SplitN(t, "=", 2)
				if len(parts) == 2 {
					dev.Txt[parts[0]] = parts[1]
				}
			}
			if len(entry.AddrIPv4) > 0 {
				dev.IP = entry.AddrIPv4[0].String()
			}
			if dev.IP != "" {
				select {
				case out <- dev:
				case <-ctx.Done():
					return
				}
			}
		}
	}()

	_ = resolver.Browse(ctx, "_qsync._tcp", "local.", entries)
}

func scanSubnets(ctx context.Context, port int, out chan<- DiscoveredDevice) {
	subnets := getLocalSubnetPrefixes()
	if len(subnets) == 0 {
		return
	}

	var wg sync.WaitGroup
	semaphore := make(chan struct{}, 100) // 100 concurrent fast probes

	for _, prefix := range subnets {
		for i := 1; i <= 254; i++ {
			select {
			case <-ctx.Done():
				return
			default:
			}

			ip := fmt.Sprintf("%s.%d", prefix, i)
			wg.Add(1)
			semaphore <- struct{}{}

			go func(targetIP string) {
				defer wg.Done()
				defer func() { <-semaphore }()

				addr := fmt.Sprintf("%s:%d", targetIP, port)
				dialer := net.Dialer{Timeout: 150 * time.Millisecond}
				conn, err := dialer.DialContext(ctx, "tcp", addr)
				if err != nil {
					return
				}
				_ = conn.Close()

				// Fast HTTP probe
				client := &http.Client{Timeout: 300 * time.Millisecond}
				req, err := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://%s:%d/", targetIP, port), nil)
				if err != nil {
					return
				}
				resp, err := client.Do(req)
				if err != nil {
					return
				}
				defer resp.Body.Close()

				authHeader := resp.Header.Get("Www-Authenticate")
				if resp.StatusCode == http.StatusUnauthorized && strings.Contains(authHeader, `realm="qSync"`) {
					deviceName := "qSync Android"

					// Try to fetch custom name configured on phone from /api/info
					infoReq, infoErr := http.NewRequestWithContext(ctx, "GET", fmt.Sprintf("http://%s:%d/api/info", targetIP, port), nil)
					if infoErr == nil {
						if infoResp, err := client.Do(infoReq); err == nil {
							var info struct {
								Name string `json:"name"`
							}
							if json.NewDecoder(infoResp.Body).Decode(&info) == nil && info.Name != "" {
								deviceName = info.Name
							}
							infoResp.Body.Close()
						}
					}

					dev := DiscoveredDevice{
						Name: deviceName,
						Host: targetIP,
						IP:   targetIP,
						Port: port,
					}
					select {
					case out <- dev:
					case <-ctx.Done():
					}
				}
			}(ip)
		}
	}

	wg.Wait()
}

func getLocalSubnetPrefixes() []string {
	var prefixes []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return prefixes
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		name := strings.ToLower(iface.Name)
		if strings.HasPrefix(name, "docker") || strings.HasPrefix(name, "br-") || strings.HasPrefix(name, "veth") {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, addr := range addrs {
			ipNet, ok := addr.(*net.IPNet)
			if !ok || ipNet.IP.To4() == nil || ipNet.IP.IsLoopback() {
				continue
			}

			ip := ipNet.IP.To4()
			prefix := fmt.Sprintf("%d.%d.%d", ip[0], ip[1], ip[2])
			prefixes = append(prefixes, prefix)
		}
	}

	return prefixes
}

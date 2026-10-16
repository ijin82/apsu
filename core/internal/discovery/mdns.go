package discovery

import (
	"fmt"
	"strconv"
	"sync"

	"github.com/grandcat/zeroconf"
)

type Broadcaster struct {
	mu     sync.Mutex
	server *zeroconf.Server
}

func NewBroadcaster() *Broadcaster {
	return &Broadcaster{}
}

func (b *Broadcaster) Start(serviceName string, port int) error {
	return b.StartSecure(serviceName, port, 0, "")
}

func (b *Broadcaster) StartSecure(serviceName string, port int, tlsPort int, certFP string) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.server != nil {
		b.server.Shutdown()
		b.server = nil
	}

	txtRecords := []string{
		"txtvers=1",
		"name=" + serviceName,
		"port=" + strconv.Itoa(port),
	}
	if tlsPort > 0 {
		txtRecords = append(txtRecords, "tls_port="+strconv.Itoa(tlsPort))
	}
	if certFP != "" {
		txtRecords = append(txtRecords, "cert_fp="+certFP)
	}

	server, err := zeroconf.Register(
		serviceName,
		"_apsu._tcp",
		"local.",
		port,
		txtRecords,
		nil,
	)
	if err != nil {
		return fmt.Errorf("failed to register mDNS service: %w", err)
	}

	b.server = server
	return nil
}

func (b *Broadcaster) Stop() {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.server != nil {
		b.server.Shutdown()
		b.server = nil
	}
}

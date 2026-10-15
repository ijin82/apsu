package mount

import (
	"testing"
)

func TestValidateHostAndPort(t *testing.T) {
	validCases := []struct {
		host string
		port int
	}{
		{"192.168.1.100", 8484},
		{"127.0.0.1", 80},
		{"::1", 443},
		{"2001:db8::1", 8484},
		{"my-laptop", 8484},
		{"phone.local", 8484},
		{"server.internal.corp", 9000},
	}

	for _, tc := range validCases {
		if err := validateHostAndPort(tc.host, tc.port); err != nil {
			t.Errorf("expected %s:%d to be valid, got: %v", tc.host, tc.port, err)
		}
	}

	invalidCases := []struct {
		host string
		port int
	}{
		{"", 8484},
		{"192.168.1.1; rm -rf /", 8484},
		{"192.168.1.1`whoami`", 8484},
		{"192.168.1.1$PATH", 8484},
		{"192.168.1.1\n", 8484},
		{"host name with space", 8484},
		{"192.168.1.1", 0},
		{"192.168.1.1", -1},
		{"192.168.1.1", 70000},
	}

	for _, tc := range invalidCases {
		if err := validateHostAndPort(tc.host, tc.port); err == nil {
			t.Errorf("expected %s:%d to be invalid, got nil error", tc.host, tc.port)
		}
	}
}

func TestMounterInvalidInputs(t *testing.T) {
	m := &linuxMounter{}
	if err := m.Mount("192.168.1.1; rm -rf /", 8484, "token"); err == nil {
		t.Error("expected error for malicious IP in linuxMounter.Mount")
	}
	if err := m.Unmount("malicious`ip`", 8484); err == nil {
		t.Error("expected error for malicious IP in linuxMounter.Unmount")
	}
	if _, err := m.IsMounted("bad host", 8484); err == nil {
		t.Error("expected error for malicious IP in linuxMounter.IsMounted")
	}

	dm := &darwinMounter{}
	if err := dm.Mount("192.168.1.1; id", 8484, "token"); err == nil {
		t.Error("expected error for malicious IP in darwinMounter.Mount")
	}

	wm := &windowsMounter{}
	if err := wm.Mount("192.168.1.1 & dir", 8484, "token"); err == nil {
		t.Error("expected error for malicious IP in windowsMounter.Mount")
	}
}

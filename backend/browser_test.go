package main

import (
	"net"
	"testing"
	"time"
)

func TestShouldOpenBrowser(t *testing.T) {
	tests := []struct {
		name                                             string
		version, goos, display, waylandDisplay, envValue string
		want                                             bool
	}{
		{name: "dev build defaults off", version: "dev", goos: "linux", want: false},
		{name: "release build defaults on", version: "1.2.3", goos: "darwin", want: true},
		{name: "release build on windows defaults on", version: "1.2.3", goos: "windows", want: true},
		{name: "headless linux release (server/container/systemd) defaults off", version: "1.2.3", goos: "linux", want: false},
		{name: "linux release with X11 display defaults on", version: "1.2.3", goos: "linux", display: ":0", want: true},
		{name: "linux release with Wayland display defaults on", version: "1.2.3", goos: "linux", waylandDisplay: "wayland-0", want: true},
		{name: "OPEN_BROWSER=true forces on even for a dev build", version: "dev", goos: "linux", envValue: "true", want: true},
		{name: "OPEN_BROWSER=1 forces on", version: "dev", goos: "linux", envValue: "1", want: true},
		{name: "OPEN_BROWSER=false forces off even for a release build", version: "1.2.3", goos: "darwin", envValue: "false", want: false},
		{name: "OPEN_BROWSER=0 forces off", version: "1.2.3", goos: "darwin", envValue: "0", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := shouldOpenBrowser(tt.version, tt.goos, tt.display, tt.waylandDisplay, tt.envValue); got != tt.want {
				t.Errorf("shouldOpenBrowser(%q, %q, %q, %q, %q) = %v, want %v",
					tt.version, tt.goos, tt.display, tt.waylandDisplay, tt.envValue, got, tt.want)
			}
		})
	}
}

func TestBrowserURL(t *testing.T) {
	tests := []struct {
		name string
		addr string
		tls  bool
		want string
	}{
		{name: "loopback default", addr: "127.0.0.1:8080", want: "http://127.0.0.1:8080"},
		{name: "0.0.0.0 becomes loopback", addr: "0.0.0.0:8080", want: "http://127.0.0.1:8080"},
		{name: "empty host becomes loopback", addr: ":8080", want: "http://127.0.0.1:8080"},
		{name: "IPv6 any becomes loopback", addr: "[::]:8080", want: "http://127.0.0.1:8080"},
		{name: "TLS uses https", addr: "127.0.0.1:8443", tls: true, want: "https://127.0.0.1:8443"},
		{name: "custom host preserved", addr: "192.168.1.5:8080", want: "http://192.168.1.5:8080"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := browserURL(tt.addr, tt.tls); got != tt.want {
				t.Errorf("browserURL(%q, %v) = %q, want %q", tt.addr, tt.tls, got, tt.want)
			}
		})
	}
}

func TestWaitForListen(t *testing.T) {
	t.Run("returns true once something is listening", func(t *testing.T) {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = ln.Close() }()
		if !waitForListen(ln.Addr().String(), time.Second) {
			t.Error("expected waitForListen to succeed against a live listener")
		}
	})

	t.Run("times out when nothing is listening", func(t *testing.T) {
		if waitForListen("127.0.0.1:1", 300*time.Millisecond) {
			t.Error("expected waitForListen to fail when nothing is listening")
		}
	})
}

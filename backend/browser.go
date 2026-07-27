package main

import (
	"fmt"
	"log"
	"net"
	"os/exec"
	"runtime"
	"time"
)

// browserURL turns a listen address into a URL a browser can actually load:
// a bind host of "" / "0.0.0.0" / "::" only means "every interface" to the
// OS, not something a browser can dial, so it's swapped for the loopback
// address.
func browserURL(addr string, tls bool) string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		host, port = addr, ""
	}
	switch host {
	case "", "0.0.0.0", "::":
		host = "127.0.0.1"
	}
	scheme := "http"
	if tls {
		scheme = "https"
	}
	if port == "" {
		return fmt.Sprintf("%s://%s", scheme, host)
	}
	return fmt.Sprintf("%s://%s:%s", scheme, host, port)
}

// shouldOpenBrowser decides whether to auto-open the frontend once the
// server is ready. Off by default for a `go run .`/dev build (version stays
// "dev" — see main.go) so restarting during development doesn't keep
// popping a new tab; on by default for an actual release binary, unless
// that would be a headless Linux process (a server, systemd unit, or
// Docker container — virtually always true for how this backend actually
// gets deployed there) with no X11/Wayland display to open anything on.
// OPEN_BROWSER always overrides either way.
func shouldOpenBrowser(version, goos, display, waylandDisplay, openBrowserEnv string) bool {
	switch openBrowserEnv {
	case "false", "0":
		return false
	case "true", "1":
		return true
	}
	if version == "dev" {
		return false
	}
	if goos == "linux" && display == "" && waylandDisplay == "" {
		return false
	}
	return true
}

// maybeOpenBrowser waits (in the background) for addr to accept
// connections, then opens url in the user's default browser. A no-op per
// shouldOpenBrowser's rules. Never blocks the caller and never fails
// startup — a browser that doesn't open is a minor inconvenience, not
// worth taking the server down over.
func maybeOpenBrowser(addr, url string) {
	go func() {
		if !waitForListen(addr, 10*time.Second) {
			return
		}
		if err := openBrowser(url); err != nil {
			log.Printf("could not open the browser automatically (%v) — open %s manually", err, url)
		}
	}()
}

// waitForListen polls addr until something accepts a TCP connection or
// timeout elapses. net.Listen (called moments earlier, before Serve's
// accept loop even starts) already has the OS backlog queuing connections,
// so this succeeds as soon as the port is bound — no HTTP request needed.
func waitForListen(addr string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond) //nolint:gosec // addr is our own listen address (ADDR env var / default), not attacker input
		if err == nil {
			_ = conn.Close()
			return true
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

// openBrowser shells out to the OS-native "open a URL" command. url is
// always our own browserURL() output (this server's own address), never
// user input, so there's nothing here for the command to inject.
func openBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url) //nolint:gosec
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url) //nolint:gosec
	default:
		cmd = exec.Command("xdg-open", url) //nolint:gosec
	}
	return cmd.Start()
}

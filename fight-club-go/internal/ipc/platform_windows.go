//go:build windows
// +build windows

package ipc

import (
	"fmt"
	"net"
	"time"
)

// CreatePlatformListener creates a TCP listener on localhost (Windows)
// Windows doesn't support Unix domain sockets reliably, so we use TCP localhost.
// TCP on localhost is still very fast (sub-millisecond latency).
func CreatePlatformListener(socketPath string) (net.Listener, error) {
	// Ignore socketPath on Windows, use TCP localhost instead
	listener, err := net.Listen("tcp", DefaultTCPPort)
	if err != nil {
		return nil, fmt.Errorf("listen tcp %s: %w", DefaultTCPPort, err)
	}

	return listener, nil
}

// ConnectPlatform connects to the IPC server via TCP (Windows)
func ConnectPlatform(socketPath string) (net.Conn, error) {
	// Ignore socketPath on Windows, use TCP localhost instead
	conn, err := net.DialTimeout("tcp", DefaultTCPPort, time.Second)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// GetPlatformAddress returns the address string for logging
func GetPlatformAddress(socketPath string) string {
	return DefaultTCPPort + " (TCP localhost - Windows mode)"
}

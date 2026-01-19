//go:build !windows
// +build !windows

package ipc

import (
	"fmt"
	"net"
	"os"
	"time"
)

// CreatePlatformListener creates a Unix domain socket listener (Linux/macOS)
// Unix sockets have lower latency than TCP for local IPC
func CreatePlatformListener(socketPath string) (net.Listener, error) {
	// Clean up existing socket
	if err := CleanupSocket(socketPath); err != nil {
		return nil, fmt.Errorf("cleanup socket: %w", err)
	}

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		return nil, fmt.Errorf("listen unix: %w", err)
	}

	// Set socket permissions
	if err := os.Chmod(socketPath, 0666); err != nil {
		listener.Close()
		return nil, fmt.Errorf("chmod socket: %w", err)
	}

	return listener, nil
}

// ConnectPlatform connects to the IPC socket (Unix domain socket on Linux/macOS)
func ConnectPlatform(socketPath string) (net.Conn, error) {
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// GetPlatformAddress returns the address string for logging
func GetPlatformAddress(socketPath string) string {
	return socketPath
}

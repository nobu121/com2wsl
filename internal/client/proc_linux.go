//go:build linux

package client

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// fdHeldByApp reports whether a process other than excludePID has opened the device.
func fdHeldByApp(devicePath string, excludePID int) bool {
	abs, err := filepath.EvalSymlinks(devicePath)
	if err != nil {
		abs = devicePath
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return false
	}
	self := strconv.Itoa(excludePID)
	for _, e := range entries {
		if !e.IsDir() || e.Name() == self {
			continue
		}
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		fdDir := filepath.Join("/proc", e.Name(), "fd")
		fds, err := os.ReadDir(fdDir)
		if err != nil {
			continue
		}
		for _, fd := range fds {
			link := filepath.Join(fdDir, fd.Name())
			target, err := os.Readlink(link)
			if err != nil {
				continue
			}
			target = strings.TrimSuffix(strings.TrimSpace(target), " (deleted)")
			if target == abs || target == devicePath {
				return true
			}
			if resolved, err := filepath.EvalSymlinks(target); err == nil && (resolved == abs || resolved == devicePath) {
				return true
			}
		}
	}
	return false
}

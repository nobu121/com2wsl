//go:build linux

package cmdroot

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/nobu/com2wsl/internal/client"
)

func runClient(serverAddr, linkDir string, basePort, syncSec int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if linkDir == "" {
		linkDir = filepath.Join(homeDir(), ".com2wsl")
	}

	return client.Run(ctx, client.Config{
		ServerAddr:   serverAddr,
		ControlPort:  controlPort,
		BasePort:     basePort,
		LinkDir:      linkDir,
		SyncInterval: time.Duration(syncSec) * time.Second,
	})
}

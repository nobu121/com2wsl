//go:build windows

package cmdroot

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/nobu/com2wsl/internal/serialcfg"
	"github.com/nobu/com2wsl/internal/server"
)

func runServer(bind string, controlPort, basePort, scanSec, baudRate int) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	return server.Run(ctx, server.Config{
		BindAddr:     bind,
		ControlPort:  controlPort,
		BasePort:     basePort,
		ScanInterval: time.Duration(scanSec) * time.Second,
		Serial:       serialcfg.Default8N1(baudRate),
	})
}

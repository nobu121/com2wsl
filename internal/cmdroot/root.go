package cmdroot

import (
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/nobu/com2wsl/internal/debug"
)

var (
	controlPort int
	basePort    int
	bindAddr    string
	debugMode   bool
)

func Execute() error {
	return newRootCmd().Execute()
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "com2wsl",
		Short: "Bridge Windows COM ports to WSL pseudo-serial devices",
	}

	root.PersistentFlags().IntVar(&controlPort, "control-port", 14500, "control API TCP port")
	root.PersistentFlags().IntVar(&basePort, "base-port", 14500, "base TCP port; data port = base + COM number")
	root.PersistentFlags().StringVar(&bindAddr, "bind", "0.0.0.0", "listen/bind address")
	root.PersistentFlags().BoolVarP(&debugMode, "debug", "d", false, "debug mode: log serial connect/disconnect events")

	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		debug.SetEnabled(debugMode)
	}

	root.AddCommand(newServerCmd())
	root.AddCommand(newClientCmd())
	root.AddCommand(newVersionCmd())

	return root
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("com2wsl (%s/%s)\n", runtime.GOOS, runtime.GOARCH)
		},
	}
}

func newServerCmd() *cobra.Command {
	var scanInterval, baudRate int
	cmd := &cobra.Command{
		Use:   "server",
		Short: "Run Windows server (enumerate COM ports and expose TCP)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "windows" {
				return fmt.Errorf("server must run on Windows (current: %s)", runtime.GOOS)
			}
			if baudRate <= 0 {
				return fmt.Errorf("--baud must be positive (got %d)", baudRate)
			}
			return runServer(bindAddr, controlPort, basePort, scanInterval, baudRate)
		},
	}
	cmd.Flags().IntVar(&scanInterval, "scan-interval", 2, "COM port rescan interval in seconds")
	cmd.Flags().IntVar(&baudRate, "baud", 9600, "serial baud rate (8N1)")
	return cmd
}

func newClientCmd() *cobra.Command {
	var (
		serverAddr   string
		syncInterval int
		linkDir      string
	)
	cmd := &cobra.Command{
		Use:   "client",
		Short: "Run WSL/Linux client (auto-create pseudo-serial devices)",
		RunE: func(cmd *cobra.Command, args []string) error {
			if runtime.GOOS != "linux" {
				return fmt.Errorf("client must run on Linux/WSL (current: %s)", runtime.GOOS)
			}
			return runClient(serverAddr, linkDir, basePort, syncInterval)
		},
	}
	cmd.Flags().StringVar(&serverAddr, "server", "", "server host:port (default: auto-detect Windows host)")
	cmd.Flags().IntVar(&syncInterval, "sync-interval", 3, "port list sync interval in seconds")
	cmd.Flags().StringVar(&linkDir, "link-dir", "", "directory for COM symlinks (default: ~/.com2wsl)")
	return cmd
}

func homeDir() string {
	h, err := os.UserHomeDir()
	if err != nil {
		return "."
	}
	return h
}

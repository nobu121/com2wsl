//go:build !linux

package cmdroot

import "fmt"

func runClient(serverAddr, linkDir string, basePort, syncSec int) error {
	return fmt.Errorf("client must run on Linux/WSL; build with GOOS=linux")
}

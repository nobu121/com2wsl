//go:build !windows

package cmdroot

import "fmt"

func runServer(bind string, controlPort, basePort, scanSec, baudRate int) error {
	return fmt.Errorf("server must run on Windows; build with GOOS=windows")
}

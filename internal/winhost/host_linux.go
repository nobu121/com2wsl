//go:build linux

package winhost

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"strings"
	"time"
)

// DiscoverServer returns host:port for the Windows com2wsl server control API.
func DiscoverServer(controlPort int) (string, error) {
	if v := os.Getenv("COM2WSL_SERVER"); v != "" {
		return normalizeAddr(v, controlPort), nil
	}

	candidates := []string{"127.0.0.1"}
	if ip, err := nameserverFromResolv(); err == nil && ip != "" {
		candidates = append(candidates, ip)
	}

	addr := fmt.Sprintf(":%d", controlPort)
	for _, host := range candidates {
		target := net.JoinHostPort(host, fmt.Sprintf("%d", controlPort))
		conn, err := net.DialTimeout("tcp", target, 2*time.Second)
		if err != nil {
			continue
		}
		_ = conn.Close()
		return target, nil
	}

	// return best guess for error messages
	if len(candidates) > 1 {
		return net.JoinHostPort(candidates[1], fmt.Sprintf("%d", controlPort)),
			fmt.Errorf("cannot reach com2wsl server on %s or %s%s; set COM2WSL_SERVER or start com2wsl.exe server on Windows",
				net.JoinHostPort(candidates[0], fmt.Sprintf("%d", controlPort)),
				net.JoinHostPort(candidates[1], fmt.Sprintf("%d", controlPort)),
				addr)
	}
	return net.JoinHostPort("127.0.0.1", fmt.Sprintf("%d", controlPort)),
		fmt.Errorf("cannot reach com2wsl server at 127.0.0.1:%d; set COM2WSL_SERVER or start com2wsl.exe server", controlPort)
}

func normalizeAddr(v string, controlPort int) string {
	if strings.Contains(v, ":") {
		return v
	}
	return net.JoinHostPort(v, fmt.Sprintf("%d", controlPort))
}

func nameserverFromResolv() (string, error) {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return "", err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "nameserver" {
			return fields[1], nil
		}
	}
	return "", fmt.Errorf("no nameserver in /etc/resolv.conf")
}

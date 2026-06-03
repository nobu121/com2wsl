package protocol

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

const (
	DefaultControlPort = 14500
	DefaultBasePort    = 14500
)

var comNameRe = regexp.MustCompile(`(?i)^COM(\d+)$`)

// PortInfo describes one COM port exposed by the server.
type PortInfo struct {
	Name        string `json:"name"`
	Number      int    `json:"number"`
	DataPort    int    `json:"data_port"`
	Status      string `json:"status"`
	Description string `json:"description,omitempty"`
	Error       string `json:"error,omitempty"`
}

// PortsResponse is returned by GET /api/ports.
type PortsResponse struct {
	Ports []PortInfo `json:"ports"`
}

// ParseCOMName extracts the numeric suffix from "COM4".
func ParseCOMName(name string) (int, error) {
	m := comNameRe.FindStringSubmatch(strings.TrimSpace(name))
	if m == nil {
		return 0, fmt.Errorf("invalid COM name: %s", name)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, err
	}
	return n, nil
}

// DataPort returns the TCP port for a COM number.
func DataPort(base, comNumber int) int {
	return base + comNumber
}

// FormatCOM returns "COM4" for number 4.
func FormatCOM(n int) string {
	return fmt.Sprintf("COM%d", n)
}

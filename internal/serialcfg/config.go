package serialcfg

import (
	"go.bug.st/serial"
)

// Settings holds serial parameters for opened COM ports.
type Settings struct {
	BaudRate int
	DataBits int
	Parity   serial.Parity
	StopBits serial.StopBits
}

// Default8N1 returns 8 data bits, no parity, one stop bit at the given baud rate.
// Non-positive baud falls back to 9600.
func Default8N1(baudRate int) Settings {
	if baudRate <= 0 {
		baudRate = 9600
	}
	return Settings{
		BaudRate: baudRate,
		DataBits: 8,
		Parity:   serial.NoParity,
		StopBits: serial.OneStopBit,
	}
}

func (s Settings) Mode() *serial.Mode {
	return &serial.Mode{
		BaudRate: s.BaudRate,
		DataBits: s.DataBits,
		Parity:   s.Parity,
		StopBits: s.StopBits,
	}
}

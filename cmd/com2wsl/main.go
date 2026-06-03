package main

import (
	"fmt"
	"os"

	"github.com/nobu/com2wsl/internal/cmdroot"
)

func main() {
	if err := cmdroot.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

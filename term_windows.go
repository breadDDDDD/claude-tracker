//go:build windows

package main

import (
	"os"

	"golang.org/x/sys/windows"
)

// setupConsole turns on ANSI escape processing and UTF-8 output for the
// classic Windows console, and returns a func that puts both back.
func setupConsole() func() {
	h := windows.Handle(os.Stdout.Fd())
	var mode uint32
	hasMode := windows.GetConsoleMode(h, &mode) == nil
	if hasMode {
		windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING|windows.ENABLE_PROCESSED_OUTPUT)
	}
	cp, cpErr := windows.GetConsoleOutputCP()
	windows.SetConsoleOutputCP(65001)
	return func() {
		if hasMode {
			windows.SetConsoleMode(h, mode)
		}
		if cpErr == nil {
			windows.SetConsoleOutputCP(cp)
		}
	}
}

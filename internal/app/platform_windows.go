//go:build windows

package app

import (
	"os"

	"golang.org/x/sys/windows"
)

// terminationSignals stop the server (architecture/cli.md): SIGINT
// (console Ctrl+C) and SIGTERM cancel in-flight request contexts and
// trigger the bounded shutdown.
var terminationSignals = []os.Signal{os.Interrupt, windows.SIGTERM}

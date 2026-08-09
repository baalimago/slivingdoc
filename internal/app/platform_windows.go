//go:build windows

package app

import (
	"os"

	"golang.org/x/sys/windows"
)

// terminationSignals stop the server (architecture section 17): SIGINT
// (console Ctrl+C) and SIGTERM cancel in-flight request contexts and
// trigger the bounded shutdown.
var terminationSignals = []os.Signal{os.Interrupt, windows.SIGTERM}

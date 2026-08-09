//go:build unix

package app

import (
	"os"

	"golang.org/x/sys/unix"
)

// terminationSignals stop the server (architecture section 17): SIGINT and
// SIGTERM cancel in-flight request contexts and trigger the bounded
// shutdown. The process never imports syscall; the seam uses x/sys in
// build-tagged files.
var terminationSignals = []os.Signal{os.Interrupt, unix.SIGTERM}

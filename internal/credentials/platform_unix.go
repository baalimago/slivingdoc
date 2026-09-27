//go:build unix

package credentials

import (
	"os"

	"golang.org/x/sys/unix"
)

// openFlags open the credentials file without following a final symbolic
// link, and without blocking on a substituted FIFO; the descriptor is then
// checked with fstat.
const openFlags = os.O_RDONLY | unix.O_NOFOLLOW | unix.O_NONBLOCK

func fileOwner(file *os.File) (int, error) {
	var st unix.Stat_t
	if err := unix.Fstat(int(file.Fd()), &st); err != nil {
		return 0, err
	}
	return int(st.Uid), nil
}

func pathOwner(path string) (int, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return 0, err
	}
	return int(st.Uid), nil
}

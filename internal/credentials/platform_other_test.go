//go:build !unix

package credentials

import "errors"

func mkfifo(string) error { return errors.New("no FIFOs on this platform") }

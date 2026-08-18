// Command lease owns the one SeaweedFS testcontainer shared by make test.
// It is test infrastructure only: production packages never invoke it.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"

	"github.com/baalimago/slivingdoc/internal/tests3"
)

func main() {
	readyFile := flag.String("ready-file", "", "path where the ready endpoint is written")
	flag.Parse()
	if *readyFile == "" || flag.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: tests3-lease --ready-file <path>")
		os.Exit(2)
	}

	endpoint, err := tests3.Endpoint()
	if err != nil {
		_ = writeReady(*readyFile, "error: "+err.Error())
		fmt.Fprintf(os.Stderr, "s3 integration unavailable: %v\nDocker is required to run this suite; start the daemon and re-run.\n", err)
		os.Exit(1)
	}
	if err := writeReady(*readyFile, endpoint); err != nil {
		fmt.Fprintf(os.Stderr, "tests3 lease: write ready endpoint: %v\n", err)
		tests3.Terminate()
		os.Exit(1)
	}

	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	<-interrupt
	tests3.Terminate()
}

func writeReady(path, endpoint string) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".tests3-endpoint-")
	if err != nil {
		return err
	}
	temp := file.Name()
	defer os.Remove(temp)
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := fmt.Fprintln(file, endpoint); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(temp, path)
}

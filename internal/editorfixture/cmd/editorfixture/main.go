// Command editorfixture generates and checks the protocol fixtures of the
// browser editor (see package editorfixture).
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/baalimago/slivingdoc/internal/editorfixture"
	"github.com/baalimago/slivingdoc/internal/git2"
)

func main() {
	os.Exit(run())
}

func run() int {
	eng := git2.New()
	if err := eng.Open(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer eng.Close()
	if err := editorfixture.Main(context.Background(), eng, os.Args[1:], os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

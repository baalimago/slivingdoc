package editorfixture

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrUsage reports arguments Main does not accept.
var ErrUsage = errors.New("usage: editorfixture generate|conform <directory>")

// Main runs the tool: `generate <dir>` writes the scenario fixtures into
// dir, `conform <dir>` checks every manifest and pack below dir. It writes
// a one-line summary to stdout and returns every failure.
func Main(ctx context.Context, eng Engine, args []string, stdout io.Writer) error {
	if len(args) != 2 {
		return ErrUsage
	}
	work, err := os.MkdirTemp("", "editorfixture-*")
	if err != nil {
		return fmt.Errorf("editorfixture: scratch directory: %w", err)
	}
	defer os.RemoveAll(work)
	switch args[0] {
	case "generate":
		if err := os.MkdirAll(args[1], 0o755); err != nil {
			return fmt.Errorf("editorfixture: create %s: %w", args[1], err)
		}
		scenarios := Scenarios()
		if err := Generate(ctx, eng, work, args[1], scenarios); err != nil {
			return err
		}
		_, err := fmt.Fprintf(stdout, "generated %d scenarios in %s\n", len(scenarios), args[1])
		return err
	case "conform":
		report, err := Conform(eng, work, args[1])
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(stdout, "conformed %d manifests, %d chains, %d packs in %s\n", report.Manifests, report.Chains, report.Packs, args[1])
		return err
	default:
		return ErrUsage
	}
}

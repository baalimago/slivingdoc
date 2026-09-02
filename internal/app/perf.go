package app

import (
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"time"
)

// perfEnv gates whole-command performance capture. Profiling is an
// operator concern like logging: it is read from the environment, not
// from flags, so it works identically for every command.
const perfEnv = "DEBUG_PERF"

// StartPerf begins performance capture for one whole command when
// DEBUG_PERF asks for it and returns the function that finishes the
// capture; the caller runs it after the command returns. The capture is
// a CPU profile and an execution trace recorded across the command, plus
// a heap profile written at stop. Each run creates its own timestamped
// directory under the resolved base (see perfBase), so repeated
// benchmark runs never overwrite each other and 'go tool pprof
// -diff_base' can compare them.
//
// Profiling is diagnostic plumbing (see NewLogger): every failure is a
// warning through logger, never a refusal, and the returned function is
// never nil. The capture start, the finished file paths, and the
// analysis commands are reported through logger on stderr, so stdout
// stays protocol-only.
func StartPerf(environment []string, logger *slog.Logger) func() {
	base := perfBase(environ(environment)[perfEnv])
	if base == "" {
		return func() {}
	}
	capture, err := startPerf(base)
	if err != nil {
		logger.Warn("skipping the performance capture", "error", err)
		return func() {}
	}
	logger.Info("performance capture started", "dir", capture.dir)
	return func() {
		if err := capture.stop(); err != nil {
			logger.Warn("performance capture incomplete", "error", err)
		}
		logger.Info("performance capture written",
			"cpu", capture.cpuPath,
			"heap", capture.heapPath,
			"trace", capture.tracePath,
			"analyze", "go tool pprof "+capture.cpuPath+" | go tool trace "+capture.tracePath)
	}
}

// perfBase resolves the DEBUG_PERF value to the capture base directory.
// Empty, "0", and "false" disable the capture; "1" and "true" use
// slivingdoc-perf under the operating-system temporary directory; any
// other value is the base directory itself. The default is deliberately
// not the working directory: pull and commit often run inside the
// notebook, and a binary profile there would be rejected by the next
// commit as non-text content.
func perfBase(value string) string {
	switch value {
	case "", "0", "false":
		return ""
	case "1", "true":
		return filepath.Join(os.TempDir(), "slivingdoc-perf")
	default:
		return value
	}
}

// perfCapture is one live capture: the run directory and the two files
// that record continuously while the command runs.
type perfCapture struct {
	dir       string
	cpuPath   string
	heapPath  string
	tracePath string
	cpu       *os.File
	trace     *os.File
}

// startPerf creates the run directory and starts the CPU profile and the
// execution trace. Any failure undoes what already started, so a partial
// capture never stays running behind a warning.
func startPerf(base string) (*perfCapture, error) {
	if err := os.MkdirAll(base, 0o755); err != nil {
		return nil, fmt.Errorf("app: create the %s base directory: %w", perfEnv, err)
	}
	dir, err := os.MkdirTemp(base, time.Now().UTC().Format("20060102-150405")+"-*")
	if err != nil {
		return nil, fmt.Errorf("app: create the capture run directory: %w", err)
	}
	c := &perfCapture{
		dir:       dir,
		cpuPath:   filepath.Join(dir, "cpu.pprof"),
		heapPath:  filepath.Join(dir, "heap.pprof"),
		tracePath: filepath.Join(dir, "trace.out"),
	}
	if c.cpu, err = os.Create(c.cpuPath); err != nil {
		return nil, fmt.Errorf("app: create the CPU profile: %w", err)
	}
	if c.trace, err = os.Create(c.tracePath); err != nil {
		c.cpu.Close()
		return nil, fmt.Errorf("app: create the execution trace: %w", err)
	}
	if err = pprof.StartCPUProfile(c.cpu); err != nil {
		c.cpu.Close()
		c.trace.Close()
		return nil, fmt.Errorf("app: start the CPU profile: %w", err)
	}
	if err = trace.Start(c.trace); err != nil {
		pprof.StopCPUProfile()
		c.cpu.Close()
		c.trace.Close()
		return nil, fmt.Errorf("app: start the execution trace: %w", err)
	}
	return c, nil
}

// stop finishes the capture: it stops the continuous recordings, flushes
// their files, and writes the heap profile. Every failure is collected,
// so one bad file does not hide the others.
func (c *perfCapture) stop() error {
	pprof.StopCPUProfile()
	trace.Stop()
	return errors.Join(c.closeFile("CPU profile", c.cpu),
		c.closeFile("execution trace", c.trace),
		c.writeHeap())
}

// closeFile flushes one continuous recording to disk.
func (c *perfCapture) closeFile(what string, f *os.File) error {
	if err := f.Close(); err != nil {
		return fmt.Errorf("app: close the %s: %w", what, err)
	}
	return nil
}

// writeHeap records the live heap at the end of the command. The
// explicit collection updates the heap statistics first, so the profile
// shows what the command retained rather than garbage awaiting the next
// cycle.
func (c *perfCapture) writeHeap() error {
	f, err := os.Create(c.heapPath)
	if err != nil {
		return fmt.Errorf("app: create the heap profile: %w", err)
	}
	runtime.GC()
	err = pprof.WriteHeapProfile(f)
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("app: write the heap profile: %w", err)
	}
	return nil
}

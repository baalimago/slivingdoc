package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestPerfBase pins the DEBUG_PERF value grammar: off values, the
// temporary-directory default, and an explicit base directory.
func TestPerfBase(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		value string
		want  string
	}{
		{value: "", want: ""},
		{value: "0", want: ""},
		{value: "false", want: ""},
		{value: "1", want: filepath.Join(os.TempDir(), "slivingdoc-perf")},
		{value: "true", want: filepath.Join(os.TempDir(), "slivingdoc-perf")},
		{value: filepath.Join("some", "dir"), want: filepath.Join("some", "dir")},
	} {
		if got := perfBase(row.value); got != row.want {
			t.Errorf("perfBase(%q) = %q, want %q", row.value, got, row.want)
		}
	}
}

// TestStartPerfDisabledIsSilent proves an off value is a complete no-op:
// no record on the logger and a stop function that is safe to call.
func TestStartPerfDisabledIsSilent(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"", "0", "false"} {
		var out strings.Builder
		logger, err := NewLogger([]string{"NO_COLOR=1"}, &out)
		if err != nil {
			t.Fatalf("NewLogger() = %v", err)
		}
		stop := StartPerf([]string{perfEnv + "=" + value}, logger)
		stop()
		if out.Len() != 0 {
			t.Errorf("DEBUG_PERF=%q logged %q, want silence", value, out.String())
		}
	}
}

// TestStartPerfWritesProfiles proves one enabled capture produces its own
// run directory holding a non-empty CPU profile, heap profile, and
// execution trace, and reports the capture on the logger.
//
// Not parallel: the CPU profile and the execution trace are
// process-global recordings.
func TestStartPerfWritesProfiles(t *testing.T) {
	base := filepath.Join(t.TempDir(), "perf")
	var out strings.Builder
	logger, err := NewLogger([]string{"NO_COLOR=1"}, &out)
	if err != nil {
		t.Fatalf("NewLogger() = %v", err)
	}
	stop := StartPerf([]string{perfEnv + "=" + base}, logger)
	stop()

	runs, err := os.ReadDir(base)
	if err != nil {
		t.Fatalf("read the capture base: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("capture runs = %d, want exactly one per invocation", len(runs))
	}
	dir := filepath.Join(base, runs[0].Name())
	for _, name := range []string{"cpu.pprof", "heap.pprof", "trace.out"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if info.Size() == 0 {
			t.Errorf("%s is empty", name)
		}
	}
	logs := out.String()
	for _, want := range []string{"performance capture started", "performance capture written", dir} {
		if !strings.Contains(logs, want) {
			t.Errorf("logs = %q, want them to contain %q", logs, want)
		}
	}
}

// TestStartPerfSecondCaptureWarns proves a capture that cannot start —
// here because the CPU profile is already recording — is a warning and a
// safe no-op, never a refusal.
//
// Not parallel: the CPU profile is a process-global recording.
func TestStartPerfSecondCaptureWarns(t *testing.T) {
	base := t.TempDir()
	logger, err := NewLogger([]string{"NO_COLOR=1"}, &strings.Builder{})
	if err != nil {
		t.Fatalf("NewLogger() = %v", err)
	}
	stopFirst := StartPerf([]string{perfEnv + "=" + base}, logger)
	defer stopFirst()

	var out strings.Builder
	warned, err := NewLogger([]string{"NO_COLOR=1"}, &out)
	if err != nil {
		t.Fatalf("NewLogger() = %v", err)
	}
	stopSecond := StartPerf([]string{perfEnv + "=" + base}, warned)
	stopSecond()
	if !strings.Contains(out.String(), "skipping the performance capture") {
		t.Fatalf("logs = %q, want the skip warning", out.String())
	}
}

// TestStartPerfBadBaseWarns proves an unusable base directory — a path
// occupied by a regular file — degrades to a warning, keeping the
// command itself untouched.
func TestStartPerfBadBaseWarns(t *testing.T) {
	t.Parallel()
	occupied := filepath.Join(t.TempDir(), "occupied")
	if err := os.WriteFile(occupied, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("write the blocking file: %v", err)
	}
	var out strings.Builder
	logger, err := NewLogger([]string{"NO_COLOR=1"}, &out)
	if err != nil {
		t.Fatalf("NewLogger() = %v", err)
	}
	stop := StartPerf([]string{perfEnv + "=" + occupied}, logger)
	stop()
	if !strings.Contains(out.String(), "skipping the performance capture") {
		t.Fatalf("logs = %q, want the skip warning", out.String())
	}
}

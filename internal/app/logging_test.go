package app

import (
	"log/slog"
	"strings"
	"testing"
)

// TestNewLoggerAppliesPerModuleLevels proves the documented LOG_LEVEL
// grammar reaches the handler: each module filters at its own level and
// everything else takes the bare default.
func TestNewLoggerAppliesPerModuleLevels(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	logger, err := NewLogger([]string{`LOG_LEVEL=cli=warn,mcp=debug,info`, "NO_COLOR=1"}, &out)
	if err != nil {
		t.Fatalf("NewLogger() = %v", err)
	}

	Module(logger, ModuleCLI).Info("cli info is below warn")
	Module(logger, ModuleCLI).Warn("cli warn is emitted")
	Module(logger, ModuleMCP).Debug("mcp debug is emitted")
	Module(logger, ModuleApp).Debug("app debug is below info")
	Module(logger, ModuleApp).Info("app info is emitted")

	got := out.String()
	for _, want := range []string{"cli warn is emitted", "mcp debug is emitted", "app info is emitted"} {
		if !strings.Contains(got, want) {
			t.Fatalf("log = %q, want it to contain %q", got, want)
		}
	}
	for _, unwanted := range []string{"cli info is below warn", "app debug is below info"} {
		if strings.Contains(got, unwanted) {
			t.Fatalf("log = %q, must not contain %q", got, unwanted)
		}
	}
}

// TestNewLoggerRecordShape proves every record carries a timestamp, a level,
// and its module, which is what makes a captured host log usable.
func TestNewLoggerRecordShape(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	logger, err := NewLogger([]string{"NO_COLOR=1"}, &out)
	if err != nil {
		t.Fatalf("NewLogger() = %v", err)
	}
	Module(logger, ModuleMCP).Info("tool call started", "mcpReqID", "abc123")

	line := out.String()
	for _, want := range []string{"time=", "level=INFO", `msg="tool call started"`, "module=mcp", "mcpReqID=abc123"} {
		if !strings.Contains(line, want) {
			t.Fatalf("record = %q, want %q", line, want)
		}
	}
	if !strings.HasSuffix(line, "\n") {
		t.Fatalf("record = %q, want a trailing newline", line)
	}
}

// TestNewLoggerInvalidLevelIsNotFatal proves a malformed LOG_LEVEL reports
// the problem but still returns a working Info logger. Refusing to start
// over a typo in diagnostic plumbing turns it into an outage.
func TestNewLoggerInvalidLevelIsNotFatal(t *testing.T) {
	t.Parallel()
	var out strings.Builder
	logger, err := NewLogger([]string{"LOG_LEVEL=cli=verbose", "NO_COLOR=1"}, &out)
	if err == nil {
		t.Fatal("NewLogger() = nil error, want the invalid level reported")
	}
	if !strings.Contains(err.Error(), "LOG_LEVEL") || !strings.Contains(err.Error(), "verbose") {
		t.Fatalf("error = %v, want it to name LOG_LEVEL and the bad value", err)
	}
	if logger == nil {
		t.Fatal("NewLogger() = nil logger; a bad level must not disable logging")
	}
	Module(logger, ModuleCLI).Info("still logging")
	Module(logger, ModuleCLI).Debug("below the info default")
	if !strings.Contains(out.String(), "still logging") {
		t.Fatalf("log = %q, want the fallback Info level to emit", out.String())
	}
	if strings.Contains(out.String(), "below the info default") {
		t.Fatalf("log = %q, want the fallback to be Info, not Debug", out.String())
	}
}

// TestNewLoggerHonoursNoColor proves any non-empty NO_COLOR removes the ANSI
// escapes, and that color is on otherwise.
func TestNewLoggerHonoursNoColor(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name    string
		env     []string
		colored bool
	}{
		{name: "unset is coloured", colored: true},
		{name: "empty is coloured", env: []string{"NO_COLOR="}, colored: true},
		{name: "any value disables", env: []string{"NO_COLOR=1"}},
		{name: "the literal false also disables", env: []string{"NO_COLOR=false"}},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			var out strings.Builder
			logger, err := NewLogger(row.env, &out)
			if err != nil {
				t.Fatalf("NewLogger() = %v", err)
			}
			Module(logger, ModuleApp).Warn("hello")
			if got := strings.Contains(out.String(), "\033["); got != row.colored {
				t.Fatalf("record = %q, colour = %v, want %v", out.String(), got, row.colored)
			}
		})
	}
}

// TestNewLoggerNilWriterDiscards proves the constructor never panics on a
// nil writer, which the zero ProcessOptions produces.
func TestNewLoggerNilWriterDiscards(t *testing.T) {
	t.Parallel()
	logger, err := NewLogger(nil, nil)
	if err != nil {
		t.Fatalf("NewLogger() = %v", err)
	}
	Module(logger, ModuleApp).Error("must not panic")
}

// TestModuleNilLoggerDiscards proves Module never returns nil, so callers
// log unconditionally.
func TestModuleNilLoggerDiscards(t *testing.T) {
	t.Parallel()
	got := Module(nil, ModuleApp)
	if got == nil {
		t.Fatal("Module(nil) = nil, want a discarding logger")
	}
	got.Error("must not panic")
	if got.Enabled(t.Context(), slog.LevelError) != true {
		t.Log("discard logger reports disabled; acceptable, it writes nowhere")
	}
}

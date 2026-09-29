package tui

import (
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"
)

func env(pairs map[string]string) func(string) string {
	return func(name string) string { return pairs[name] }
}

func TestStylePaints(t *testing.T) {
	t.Parallel()
	plain, basic, full := New(Plain, TrueColor), New(Styled, Basic), New(Styled, TrueColor)
	for _, row := range []struct {
		name string
		got  string
		want string
	}{
		{"plain brand", plain.Brand("x"), "x"},
		{"plain mark", plain.Mark(Done) + "x", "x"},
		{"plain header", plain.Header("login", "site"), ""},
		{"basic brand", basic.Brand("x"), "\x1b[34mx\x1b[0m"},
		{"basic good", basic.Good("x"), "\x1b[32mx\x1b[0m"},
		{"basic warn", basic.Warn("x"), "\x1b[33mx\x1b[0m"},
		{"basic bad", basic.Bad("x"), "\x1b[31mx\x1b[0m"},
		{"basic dim", basic.Dim("x"), "\x1b[2mx\x1b[0m"},
		{"basic bold", basic.Bold("x"), "\x1b[1mx\x1b[0m"},
		{"empty stays empty", basic.Brand(""), ""},
		{"true brand", full.Brand("x"), "\x1b[38;2;90;162;255mx\x1b[0m"},
		{"true good", full.Good("x"), "\x1b[38;2;25;184;118mx\x1b[0m"},
		{"done mark", basic.Mark(Done), "\x1b[32m✓\x1b[0m "},
		{"failed mark", basic.Mark(Failed), "\x1b[31m✗\x1b[0m "},
		{"caution mark", basic.Mark(Caution), "\x1b[33m▲\x1b[0m "},
		{"next mark", basic.Mark(Next), "\x1b[34m→\x1b[0m "},
		{"brand mark", basic.Mark(Brand), "\x1b[34m◆\x1b[0m "},
		{"header", basic.Header("login", "www.slivingdoc.dev"), "\x1b[34m◆\x1b[0m \x1b[34mslivingdoc\x1b[0m \x1b[1mlogin\x1b[0m \x1b[2m· www.slivingdoc.dev\x1b[0m\n"},
		{"bare header", basic.Header("", ""), "\x1b[34m◆\x1b[0m \x1b[34mslivingdoc\x1b[0m\n"},
	} {
		if row.got != row.want {
			t.Errorf("%s = %q, want %q", row.name, row.got, row.want)
		}
	}
}

func TestDetect(t *testing.T) {
	t.Parallel()
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("open %s: %v", os.DevNull, err)
	}
	defer null.Close()
	// A pseudo-terminal master answers the terminal ioctl like a real
	// terminal; a host without one names that capability and skips.
	char, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		t.Skipf("style detection requires a pseudo-terminal: %v", err)
	}
	defer char.Close()
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() = %v", err)
	}
	defer pr.Close()
	defer pw.Close()
	for _, row := range []struct {
		name string
		out  io.Writer
		env  map[string]string
		want Style
	}{
		{"terminal", char, nil, New(Styled, Basic)},
		{"true colour terminal", char, map[string]string{"COLORTERM": "truecolor"}, New(Styled, TrueColor)},
		{"24bit terminal", char, map[string]string{"COLORTERM": "24bit"}, New(Styled, TrueColor)},
		{"NO_COLOR", char, map[string]string{"NO_COLOR": "1"}, Style{}},
		{"pipe", pw, nil, Style{}},
		{"/dev/null is a character device, not a terminal", null, nil, Style{}},
		{"a guard over a terminal", NewGuard(char), nil, New(Styled, Basic)},
		{"a nil file", (*os.File)(nil), nil, Style{}},
		{"buffer", &bytes.Buffer{}, nil, Style{}},
		{"nil", nil, nil, Style{}},
	} {
		if got := Detect(row.out, env(row.env)); got != row.want {
			t.Errorf("%s: Detect() = %+v, want %+v", row.name, got, row.want)
		}
	}
	if New(Styled, Basic).Mode() != Styled || (Style{}).Mode() != Plain {
		t.Fatal("Mode() does not report the mode")
	}
}

func TestColumnsAlignByVisibleText(t *testing.T) {
	t.Parallel()
	s := New(Styled, Basic)
	got := s.Columns("  ", []string{"SPACE", "ACCESS"}, [][]Cell{
		{{Text: "260926", Paint: s.Bold}, {Text: "read and write"}},
		{{Text: "wé"}, {Text: "read"}},
	})
	want := "  \x1b[2mSPACE\x1b[0m   \x1b[2mACCESS\x1b[0m\n" +
		"  \x1b[1m260926\x1b[0m  read and write\n" +
		"  wé      read\n"
	if got != want {
		t.Fatalf("Columns() = %q, want %q", got, want)
	}
	if got := (Style{}).Columns("", nil, [][]Cell{{{Text: "a"}, {Text: "b"}}}); got != "a  b\n" {
		t.Fatalf("plain Columns() = %q", got)
	}
}

// syncBuffer is a buffer the spinner goroutine and the test share.
type syncBuffer struct {
	ch  chan struct{}
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.ch <- struct{}{}
	defer func() { <-b.ch }()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.ch <- struct{}{}
	defer func() { <-b.ch }()
	return b.buf.String()
}

func TestSpinner(t *testing.T) {
	t.Parallel()
	t.Run("plain writes nothing", func(t *testing.T) {
		t.Parallel()
		var b bytes.Buffer
		if err := (Style{}).Spin(&b, func() string { return "waiting" }).Stop(); err != nil || b.Len() != 0 {
			t.Fatalf("plain spinner = %v, wrote %q", err, b.String())
		}
	})
	t.Run("styled redraws and clears", func(t *testing.T) {
		t.Parallel()
		b := &syncBuffer{ch: make(chan struct{}, 1)}
		calls := 0
		sp := New(Styled, Basic).Spin(b, func() string { calls++; return "waiting" })
		deadline := time.Now().Add(5 * time.Second)
		for strings.Count(b.String(), "waiting") < 2 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		if err := sp.Stop(); err != nil {
			t.Fatalf("Stop() = %v", err)
		}
		out := b.String()
		if !strings.HasPrefix(out, "\r\x1b[K  \x1b[34m⠋\x1b[0m waiting") || !strings.Contains(out, "⠙") || !strings.HasSuffix(out, "\r\x1b[K") {
			t.Fatalf("spinner wrote %q", out)
		}
	})
	t.Run("a failed write is returned", func(t *testing.T) {
		t.Parallel()
		sp := New(Styled, Basic).Spin(failingWriter{}, func() string { return "x" })
		if err := sp.Stop(); !errors.Is(err, errWrite) {
			t.Fatalf("Stop() = %v, want the write error", err)
		}
	})
}

var errWrite = errors.New("write refused")

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errWrite }

func pickRows() [][]Cell {
	return [][]Cell{{{Text: "260926"}, {Text: "read and write"}}, {{Text: "weboria"}, {Text: "read"}}}
}

func TestPick(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		name    string
		many    bool
		input   string
		want    []int
		wantErr error
	}{
		{name: "one row", input: "1\n", want: []int{1}},
		{name: "an invalid answer asks again", input: "x\n0\n", want: []int{0}},
		{name: "several rows", many: true, input: "0,1\n", want: []int{0, 1}},
		{name: "a range", many: true, input: "0:1\n", want: []int{0, 1}},
		{name: "one past the last row asks again", input: "2\n1\n", want: []int{1}},
		{name: "a range past the end asks again", many: true, input: "0:2\n0:1\n", want: []int{0, 1}},
		{name: "several rows where one is asked asks again", input: "0,1\n1\n", want: []int{1}},
		{name: "a repeated row counts once", many: true, input: "1,1,0\n", want: []int{1, 0}},
		{name: "a filter then its row", input: "/web\n0\n", want: []int{1}},
		{name: "quit skips", input: "q\n", wantErr: ErrSkipped},
		{name: "back skips", input: "b\n", wantErr: ErrSkipped},
		{name: "end of input skips", input: "", wantErr: ErrSkipped},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			var out bytes.Buffer
			got, err := (Style{}).Pick(Picker{
				Title: "Pick a default space", Header: []string{"SPACE", "ACCESS"}, Rows: pickRows(),
				Many: row.many, In: strings.NewReader(row.input), Out: &out,
			})
			if !errors.Is(err, row.wantErr) || len(got) != len(row.want) {
				t.Fatalf("Pick() = %v, %v; want %v, %v", got, err, row.want, row.wantErr)
			}
			for i := range got {
				if got[i] != row.want[i] {
					t.Fatalf("Pick() = %v, want %v", got, row.want)
				}
			}
			for _, want := range []string{"Pick a default space\n", "     SPACE    ACCESS\n", "  0  260926   read and write\n", "  1  weboria  read\n"} {
				if !strings.Contains(out.String(), want) {
					t.Fatalf("picker drew %q, want it to contain %q", out.String(), want)
				}
			}
		})
	}
}

func TestPickFiltersUnpaintedText(t *testing.T) {
	t.Parallel()
	s := New(Styled, Basic)
	rows := [][]Cell{{{Text: "alpha", Paint: s.Dim}}, {{Text: "team2", Paint: s.Bold}}}
	var out bytes.Buffer
	got, err := s.Pick(Picker{Title: "Pick", Header: []string{"SPACE"}, Rows: rows, In: strings.NewReader("/2\n0\n"), Out: &out})
	if err != nil || len(got) != 1 || got[0] != 1 {
		t.Fatalf("Pick() after /2 = %v, %v; want [1]: the dim escape of alpha must not match", got, err)
	}
	if strings.Contains(out.String(), "\x1b[2malpha") {
		t.Fatalf("picker drew a painted row: %q", out.String())
	}
}

func TestPickRefusals(t *testing.T) {
	t.Parallel()
	var out bytes.Buffer
	if _, err := (Style{}).Pick(Picker{Header: []string{"A"}, Out: &out}); !errors.Is(err, ErrSkipped) {
		t.Fatalf("empty Pick() = %v, want ErrSkipped", err)
	}
	if _, err := (Style{}).Pick(Picker{Rows: pickRows(), Out: &out}); err == nil || errors.Is(err, ErrSkipped) {
		t.Fatalf("headerless Pick() = %v, want a refusal", err)
	}
	if _, err := (Style{}).Pick(Picker{Header: []string{"A", "B"}, Rows: pickRows(), Out: failingWriter{}}); !errors.Is(err, errWrite) {
		t.Fatalf("Pick() on a failing writer = %v", err)
	}
	if _, err := (Style{}).Pick(Picker{Header: []string{"A", "B"}, Rows: pickRows(), In: strings.NewReader("2\n"), Out: failAfter(&out, "There is no row")}); !errors.Is(err, errWrite) {
		t.Fatalf("Pick() whose notice cannot be drawn = %v", err)
	}
}

// failAfterWriter refuses the first write that contains marker.
type failAfterWriter struct {
	w      io.Writer
	marker string
}

func failAfter(w io.Writer, marker string) io.Writer { return failAfterWriter{w, marker} }

func (f failAfterWriter) Write(p []byte) (int, error) {
	if strings.Contains(string(p), f.marker) {
		return 0, errWrite
	}
	return f.w.Write(p)
}

func TestGuardClearsTheSpinnerLineFirst(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	g := NewGuard(&b)
	if err := g.showStatus("\r\x1b[K  ⠋ Pulling"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(g, "WARN checkpoint failed\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(g, "next\n"); err != nil {
		t.Fatal(err)
	}
	if err := g.clearStatus(); err != nil {
		t.Fatal(err)
	}
	if want := "\r\x1b[K  ⠋ Pulling\r\x1b[KWARN checkpoint failed\nnext\n"; b.String() != want {
		t.Fatalf("guarded stream = %q, want %q", b.String(), want)
	}
	if g.Fd() != ^uintptr(0) || IsTerminal(g) {
		t.Fatal("a guard over a buffer has a descriptor")
	}
	if err := g.showStatus("x"); err != nil {
		t.Fatal(err)
	}
	if err := g.clearStatus(); err != nil || !strings.HasSuffix(b.String(), "x\r\x1b[K") {
		t.Fatalf("clearStatus() = %v, stream %q", err, b.String())
	}
	failing := NewGuard(failingWriter{})
	if err := failing.showStatus("x"); !errors.Is(err, errWrite) {
		t.Fatalf("showStatus() on a failing stream = %v", err)
	}
	failing.status = true
	if _, err := failing.Write([]byte("y")); !errors.Is(err, errWrite) {
		t.Fatalf("Write() on a failing stream = %v", err)
	}
}

func TestSpinnerOnAGuard(t *testing.T) {
	t.Parallel()
	b := &syncBuffer{ch: make(chan struct{}, 1)}
	g := NewGuard(b)
	sp := New(Styled, Basic).Spin(g, func() string { return "waiting" })
	deadline := time.Now().Add(5 * time.Second)
	for !strings.Contains(b.String(), "waiting") && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := io.WriteString(g, "log\n"); err != nil {
		t.Fatal(err)
	}
	if err := sp.Stop(); err != nil {
		t.Fatalf("Stop() = %v", err)
	}
	if out := b.String(); !strings.Contains(out, "waiting\r\x1b[Klog\n") {
		t.Fatalf("guarded spinner wrote %q", out)
	}
}

func TestFit(t *testing.T) {
	t.Parallel()
	dim := New(Styled, Basic).Dim
	for _, row := range []struct {
		name, label string
		width       int
		want        string
	}{
		{name: "fits", label: "Pulling ~/notes", width: 20, want: "Pulling ~/notes"},
		{name: "no width", label: "Pulling ~/notes", width: 0, want: "Pulling ~/notes"},
		{name: "exactly the width", label: "abcde", width: 5, want: "abcde"},
		{name: "cut", label: "abcdefgh", width: 5, want: "abcd…\x1b[0m"},
		{name: "escapes take no column", label: "ab" + dim("cdefgh"), width: 5, want: "ab\x1b[2mcd…\x1b[0m"},
		{name: "a painted tail that fits", label: "ab" + dim("cd"), width: 4, want: "ab" + dim("cd")},
	} {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			if got := fit(row.label, row.width); got != row.want {
				t.Fatalf("fit(%q, %d) = %q, want %q", row.label, row.width, got, row.want)
			}
		})
	}
}

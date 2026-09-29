package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/baalimago/go_away_boilerplate/pkg/cmd"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// router routes one command line over the command map and owns every line
// it prints itself: the home screen, the usage, a command's help, and the
// one-line error of a refused or failed command (architecture/tui.md).
type router struct {
	commands map[string]cmd.Command
	opts     app.ProcessOptions
	stdout   io.Writer
	stderr   io.Writer
	// out and err are the styles of stdout and stderr.
	out tui.Style
	err tui.Style
}

func newRouter(commands map[string]cmd.Command, opts app.ProcessOptions) router {
	stderr := opts.ErrOut()
	getenv := app.EnvLookup(opts.Env)
	return router{
		commands: commands, opts: opts, stdout: opts.Out(), stderr: stderr,
		out: tui.Detect(opts.Out(), getenv), err: tui.Detect(stderr, getenv),
	}
}

// run parses args (the program name first), then calls the command's
// Setup and Run, and returns the exit code: 0 on success and for help, 1
// for everything else.
func (r router) run(ctx context.Context, args []string) int {
	rest := args[1:]
	idx := slices.IndexFunc(rest, func(a string) bool { return !strings.HasPrefix(a, "-") })
	if idx < 0 {
		if len(rest) > 0 && slices.ContainsFunc(rest, isHelpFlag) {
			r.usage()
			return 0
		}
		if len(rest) == 1 && rest[0] == "--version" {
			command, _ := r.lookup("version")
			if command == nil {
				r.fail(errors.New("--version: the version command is missing"))
				return 1
			}
			if err := command.Run(ctx); err != nil {
				r.fail(err)
				return 1
			}
			return 0
		}
		if len(rest) > 0 {
			r.fail(fmt.Errorf("give a command before %s", rest[0]))
		}
		r.home()
		return 1
	}
	name := rest[idx]
	if name == "help" {
		// "help <command>" is that command's help; "help" alone the usage.
		if len(rest) > idx+1 {
			command, canonical := r.lookup(rest[idx+1])
			if command == nil {
				r.fail(fmt.Errorf("%q is not a slivingdoc command", rest[idx+1]))
				r.usage()
				return 1
			}
			r.help(canonical, command.Help())
			return 0
		}
		r.usage()
		return 0
	}
	command, canonical := r.lookup(name)
	if command == nil {
		r.fail(fmt.Errorf("%q is not a slivingdoc command", name))
		r.usage()
		return 1
	}
	flags := append(slices.Clone(rest[:idx]), rest[idx+1:]...)
	if err := command.Flagset().Parse(flags); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			r.help(canonical, command.Help())
			return 0
		}
		r.fail(fmt.Errorf("%s: %w; run 'slivingdoc %s -h' for its flags", canonical, err, canonical))
		return 1
	}
	if err := command.Setup(context.Background()); err != nil {
		r.fail(err)
		return 1
	}
	if err := command.Run(ctx); err != nil {
		r.fail(err)
		return 1
	}
	return 0
}

func isHelpFlag(a string) bool { return a == "-h" || a == "-help" || a == "--help" }

// lookup finds a command by its name or shortcut and returns it with its
// canonical name, the part of its map key before the "|".
func (r router) lookup(name string) (cmd.Command, string) {
	for key, command := range r.commands {
		names := strings.Split(key, "|")
		if slices.Contains(names, name) {
			return command, names[0]
		}
	}
	return nil, ""
}

// fail prints one error line on stderr: "error: <message>" when plain,
// a red cross before the message on a terminal. A refusal is always
// exactly one line, so scripts can read it whole.
func (r router) fail(err error) {
	s := r.err
	msg := strings.ReplaceAll(err.Error(), "\n", " ")
	if s.Mode() == tui.Styled {
		fmt.Fprintf(r.stderr, "%s%s\n", s.Mark(tui.Failed), msg)
		return
	}
	fmt.Fprintf(r.stderr, "error: %s\n", msg)
}

// help prints a command's help on stdout. On a terminal the first line
// becomes the slivingdoc header and the section titles are painted.
func (r router) help(name, text string) {
	io.WriteString(r.stdout, styleHelp(r.out, name, text))
}

// styleHelp paints a help text: its "slivingdoc <name> - <summary>" first
// line becomes the header, and every unindented line ending in ":" (Usage:,
// Flags:, Commands:) is a section title.
func styleHelp(s tui.Style, name, text string) string {
	if s.Mode() != tui.Styled {
		return text
	}
	lines := strings.Split(text, "\n")
	if summary, ok := strings.CutPrefix(lines[0], "slivingdoc "+name+" - "); ok && name != "" {
		lines[0] = strings.TrimSuffix(s.Header(name, summary), "\n")
	} else if summary, ok := strings.CutPrefix(lines[0], "slivingdoc - "); ok {
		lines[0] = strings.TrimSuffix(s.Header("", summary), "\n")
	}
	for i, line := range lines {
		if i > 0 && strings.HasSuffix(line, ":") && !strings.HasPrefix(line, " ") && !strings.Contains(line, " ") {
			lines[i] = s.Brand(line)
		}
	}
	return strings.Join(lines, "\n")
}

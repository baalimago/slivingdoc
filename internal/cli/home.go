package cli

import (
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/baalimago/slivingdoc/internal/app"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// commandGroup is one heading of the home screen and the commands under
// it, by canonical name.
type commandGroup struct {
	title    string
	commands []string
}

// commandGroups orders the home screen. TestCommandGroupsCoverTheMap
// fails when a command is missing here.
var commandGroups = []commandGroup{
	{title: "Sync", commands: []string{"pull", "commit", "status", "log"}},
	{title: "Agents", commands: []string{"serve"}},
	{title: "Account", commands: []string{"login", "space", "logout"}},
	{title: "About", commands: []string{"version"}},
}

// usage prints the full usage on stdout: the Usage text with the command
// table, painted on a terminal.
func (r router) usage() {
	s := r.out
	io.WriteString(r.stdout, styleHelp(s, "", fmt.Sprintf(Usage, r.commandTable(s))+"\n"))
}

// home prints what a bare slivingdoc shows. On a terminal that is the
// home screen: the header, the stored logins with their default spaces
// (read from the credentials file only, nothing is sent), and the
// commands by group. Elsewhere it is the usage.
func (r router) home() {
	s := r.out
	if s.Mode() != tui.Styled {
		r.usage()
		return
	}
	var b strings.Builder
	b.WriteString(s.Header("", "v"+app.Version+" · shared notes for agents"))
	b.WriteByte('\n')
	b.WriteString(r.loginStatus(s))
	b.WriteByte('\n')
	var rows [][]tui.Cell
	for _, g := range commandGroups {
		for i, name := range g.commands {
			title := ""
			if i == 0 {
				title = g.title
			}
			command, _ := r.lookup(name)
			rows = append(rows, []tui.Cell{{Text: title, Paint: s.Bold}, {Text: name, Paint: s.Brand}, {Text: command.Describe()}})
		}
	}
	b.WriteString(s.Columns("  ", nil, rows))
	b.WriteString("\n  " + s.Dim("slivingdoc <command> -h for its flags · slivingdoc help for the whole guide") + "\n")
	io.WriteString(r.stdout, b.String())
}

// loginStatus is the home screen's account lines: each stored login with
// its default space, or how to log in.
func (r router) loginStatus(s tui.Style) string {
	logins, err := app.StoredLogins(r.opts)
	if err != nil {
		return "  " + s.Mark(tui.Caution) + "The stored logins cannot be read: " + err.Error() + "\n"
	}
	if len(logins) == 0 {
		return "  " + s.Dim("Not logged in to hosted storage · ") + s.Mark(tui.Next) + "slivingdoc login\n"
	}
	var b strings.Builder
	for _, l := range logins {
		where := ""
		if l.Endpoint != app.DefaultHostedEndpoint {
			where = " at " + l.Endpoint
		}
		if l.Expired {
			fmt.Fprintf(&b, "  %s%s%s %s\n", s.Mark(tui.Caution), s.Bold(l.Account), where,
				s.Dim("· the login expired · ")+s.Mark(tui.Next)+"slivingdoc login")
			continue
		}
		fmt.Fprintf(&b, "  %sLogged in as %s%s %s\n", s.Mark(tui.Done), s.Bold(l.Account), where,
			s.Dim("· "+l.Access.Describe()+" · "+l.Expires.Describe()))
		if l.DefaultSpace == "" {
			fmt.Fprintf(&b, "  %sNo default space · %sslivingdoc space\n", s.Mark(tui.Caution), s.Mark(tui.Next))
			continue
		}
		fmt.Fprintf(&b, "  %sDefault space %s\n", s.Mark(tui.Done), s.Bold(l.DefaultSpace))
	}
	return b.String()
}

// commandTable is the Usage's command listing: one line per command,
// sorted by name, with its shortcut and description.
func (r router) commandTable(s tui.Style) string {
	keys := make([]string, 0, len(r.commands))
	for key := range r.commands {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	rows := make([][]tui.Cell, 0, len(keys))
	for _, key := range keys {
		rows = append(rows, []tui.Cell{{Text: key, Paint: s.Brand}, {Text: r.commands[key].Describe()}})
	}
	return s.Columns("  ", nil, rows)
}

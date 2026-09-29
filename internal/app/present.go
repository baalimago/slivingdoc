package app

import (
	"errors"
	"io"
	"strings"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// The presentation the login, space and logout commands share
// (architecture/tui.md): the styles of their two streams, the spaces
// table, and the pickers. The plain renderings are the lines these
// commands printed before they had a styled form, so scripts see no
// change.

// errStyle is the style of the command's stderr, where prompts and
// progress go.
func (o ProcessOptions) errStyle() tui.Style { return o.styleOf(o.ErrOut()) }

// outStyle is the style of the command's stdout, where results go.
func (o ProcessOptions) outStyle() tui.Style { return o.styleOf(o.Out()) }

func (o ProcessOptions) styleOf(out io.Writer) tui.Style {
	if o.Style != nil {
		return o.Style(out)
	}
	return tui.Detect(out, EnvLookup(o.Env))
}

// pickable reports whether a picker may replace a listing: a person can
// answer (stdin and stderr are a terminal) and stdout is a terminal too,
// so a listing piped elsewhere stays a listing. The Terminal seam, when
// set, decides alone.
func (o ProcessOptions) pickable() TerminalState {
	if o.Terminal != nil {
		return o.Terminal()
	}
	if o.terminal() == OnTerminal && tui.IsTerminal(o.Out()) {
		return OnTerminal
	}
	return NoTerminal
}

// spacesTable is the styled listing of spaces: name, access and owner,
// with the default space marked. The owner is the owner's email, never
// "you": whoever approves a code decides the login, so the owner is what
// shows someone else's approval.
func spacesTable(s tui.Style, indent string, spaces []sitelogin.Space, def string) string {
	return s.Columns(indent, []string{"SPACE", "ACCESS", "OWNER", ""}, spaceRows(s, spaces, def))
}

func spaceRows(s tui.Style, spaces []sitelogin.Space, def string) [][]tui.Cell {
	rows := make([][]tui.Cell, 0, len(spaces))
	for _, sp := range spaces {
		mark := ""
		if sp.Name == def {
			mark = "default"
		}
		rows = append(rows, []tui.Cell{
			{Text: sp.Name, Paint: s.Bold}, {Text: sp.Access.Describe()}, {Text: ownerOf(sp), Paint: s.Dim}, {Text: mark, Paint: s.Good},
		})
	}
	return rows
}

// pickSpace asks which space becomes the default and returns its name,
// or tui.ErrSkipped.
func (o ProcessOptions) pickSpace(spaces []sitelogin.Space, def string) (string, error) {
	picked, err := o.errStyle().Pick(tui.Picker{
		Title:  "Pick the default space for serve, pull and commit",
		Header: []string{"SPACE", "ACCESS", "OWNER", ""},
		Rows:   spaceRows(o.errStyle(), spaces, def),
		In:     o.Stdin,
		Out:    o.ErrOut(),
	})
	if err != nil {
		return "", err
	}
	return spaces[picked[0]].Name, nil
}

// pickLogins asks which stored logins to log out of.
func (o ProcessOptions) pickLogins(logins []credentials.Login) ([]credentials.Login, error) {
	s := o.errStyle()
	rows := make([][]tui.Cell, 0, len(logins))
	for _, l := range logins {
		rows = append(rows, []tui.Cell{{Text: l.Account, Paint: s.Bold}, {Text: l.Endpoint}, {Text: l.Site, Paint: s.Dim}})
	}
	picked, err := s.Pick(tui.Picker{
		Title:  "Log out of which logins? (\"0,1\" or the range \"0:1\" picks several)",
		Header: []string{"ACCOUNT", "STORAGE ENDPOINT", "SITE"},
		Rows:   rows,
		Many:   true,
		In:     o.Stdin,
		Out:    o.ErrOut(),
	})
	if err != nil {
		return nil, err
	}
	chosen := make([]credentials.Login, 0, len(picked))
	for _, i := range picked {
		chosen = append(chosen, logins[i])
	}
	return chosen, nil
}

// skipped reports whether err is a picker the person left.
func skipped(err error) bool { return errors.Is(err, tui.ErrSkipped) }

// line writes one styled line: mark, text, and a dimmed detail after a
// middle dot when detail is not empty.
func line(s tui.Style, mark tui.Mark, text, detail string) string {
	var b strings.Builder
	b.WriteString(s.Mark(mark))
	b.WriteString(text)
	if detail != "" {
		b.WriteString(" " + s.Dim("· "+detail))
	}
	b.WriteByte('\n')
	return b.String()
}

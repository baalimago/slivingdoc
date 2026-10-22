package app

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/settings"
	"github.com/baalimago/slivingdoc/internal/sitelogin"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// The presentation the login, space and logout commands share
// (architecture/tui.md): the styles of their two streams, the spaces
// table, and the pickers. The plain login and logout renderings are the
// lines those commands printed before they had a styled form, so scripts
// see no change; the plain space listing is new (architecture/login.md).

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

// render writes the space listing: one header per identity, one row per
// target, and the empty-group line. The styled form carries the same fields
// with colour, never a fact the plain form omits.
func (s *Space) render(groups []spaceGroup) {
	style := s.opts.outStyle()
	out := s.opts.Out()
	for i, group := range groups {
		if i > 0 {
			fmt.Fprintln(out)
		}
		fmt.Fprintln(out, groupHeaderLine(style, group.header))
		if len(group.rows) == 0 {
			if len(group.hints) == 0 {
				fmt.Fprintln(out, emptyGroupLine(group.header.Kind))
			}
			continue
		}
		for _, row := range group.rows {
			fmt.Fprintln(out, spaceRowLine(style, row, group.defaultName, group.header.Kind))
		}
	}
}

// groupHeaderLine names one identity. A hosted header names the storage
// endpoint and its account; an S3 header names the resolved endpoint,
// region, and profile, plus the access key id when the identity was
// reached. The key id is display only and is never stored.
func groupHeaderLine(style tui.Style, header SpaceHeader) string {
	if header.Kind == settings.KindS3 {
		line := fmt.Sprintf("%s  endpoint=%s  region=%s  profile=%s", style.Brand("S3"),
			endpointDisplay(header.Endpoint), regionDefault(header.Region), profileDisplay(header.Profile))
		if header.KeyID != "" {
			line += "  key=" + header.KeyID
		}
		return line
	}
	return fmt.Sprintf("%s  %s  %s", style.Brand("HOSTED"), header.Endpoint, headerAccount(header))
}

func emptyGroupLine(kind settings.Kind) string {
	if kind == settings.KindS3 {
		return "No buckets; run 'slivingdoc space new <name>' to create one."
	}
	return "No spaces yet; run 'slivingdoc space new <name>' to create one."
}

// hints writes every group's hints to stderr. A hint that reports a
// degraded or unreachable identity begins with a literal "! " in plain and
// styled output; the no-default hint keeps its text and no mark.
func (s *Space) hints(groups []spaceGroup) {
	errOut := s.opts.ErrOut()
	noDefault := make(map[settings.Kind]bool)
	for _, group := range groups {
		for _, hint := range group.hints {
			fmt.Fprintf(errOut, "! %s\n", hint)
		}
		if group.noDefault && !noDefault[group.header.Kind] {
			fmt.Fprintln(errOut, noDefaultLine(group.header.Kind))
			noDefault[group.header.Kind] = true
		}
	}
}

func noDefaultLine(kind settings.Kind) string {
	if kind == settings.KindS3 {
		return "No default bucket; run 'slivingdoc space <name>' to choose one."
	}
	return "No default space; run 'slivingdoc space <name>' to choose one."
}

// spaceRowLine is one row: the marker, the name, its directories, and,
// for a hosted row the listing returned, its access and owner. An S3
// account is already named by its group header.
func spaceRowLine(style tui.Style, row SpaceRow, defaultName string, kind settings.Kind) string {
	marker := " "
	if row.Name == defaultName {
		marker = "*"
	}
	if !row.Reachable {
		marker += "!"
	}
	text := style.Bold(row.Name)
	if dirs := directoryDisplay(row.Directories); dirs != "" {
		text += style.Dim(" (" + dirs + ")")
	}
	if row.Reachable && kind == settings.KindHosted {
		text += "  " + spaceAccess(row) + "  " + style.Dim(spaceOwner(row))
	}
	return marker + " " + text
}

// directoryDisplay bounds a target's directories to three, then +N, so
// the plain listing and the picker show a binding the same way.
func directoryDisplay(dirs []string) string {
	if len(dirs) == 0 {
		return ""
	}
	shown := dirs
	if len(shown) > 3 {
		shown = shown[:3]
	}
	text := strings.Join(shown, ", ")
	if extra := len(dirs) - len(shown); extra > 0 {
		text += fmt.Sprintf(", +%d", extra)
	}
	return text
}

// spacePicker is the terminal form of the space listing: one table whose
// TARGET column names each row's identity, and one create row after each
// configured backend (architecture/tui.md).
func (o ProcessOptions) spacePicker(choices []spaceChoice) tui.Picker {
	return tui.Picker{
		Title:  "Pick the default target",
		Header: []string{"TARGET", "NAME", "DIRECTORIES", "ACCESS", "OWNER", ""},
		Rows:   spacePickerRows(choices),
		In:     o.stdin(),
		Out:    o.ErrOut(),
	}
}

func spacePickerRows(choices []spaceChoice) [][]tui.Cell {
	rows := make([][]tui.Cell, 0, len(choices))
	for _, choice := range choices {
		rows = append(rows, spacePickerRow(choice))
	}
	return rows
}

func spacePickerRow(c spaceChoice) []tui.Cell {
	name, access, owner, mark := c.row.Name, "", "", ""
	if c.create {
		name = createRowText(c.header.Kind)
	} else {
		if !c.row.Reachable {
			name = "! " + name
		} else if c.header.Kind == settings.KindHosted {
			access, owner = spaceAccess(c.row), spaceOwner(c.row)
		}
		if c.row.Name == c.defaultName {
			mark = "default"
		}
	}
	return []tui.Cell{
		{Text: spaceTarget(c.header)},
		{Text: name},
		{Text: directoryDisplay(c.row.Directories)},
		{Text: access},
		{Text: owner},
		{Text: mark},
	}
}

// spaceTarget is the picker's name for one identity. An S3 access key id
// is display only and never stored (architecture/security.md).
func spaceTarget(header SpaceHeader) string {
	if header.Kind == settings.KindS3 {
		target := fmt.Sprintf("s3 %s %s profile=%s", endpointDisplay(header.Endpoint), regionDefault(header.Region), profileDisplay(header.Profile))
		if header.KeyID != "" {
			target += " key=" + header.KeyID
		}
		return target
	}
	return fmt.Sprintf("hosted %s %s", header.Endpoint, headerAccount(header))
}

func createRowText(kind settings.Kind) string {
	if kind == settings.KindS3 {
		return "+ Create a new S3 bucket"
	}
	return "+ Create a new hosted space"
}

func namePrompt(kind settings.Kind) string {
	if kind == settings.KindS3 {
		return "Name for the new S3 bucket: "
	}
	return "Name for the new hosted space: "
}

func spaceAccess(row SpaceRow) string {
	if row.Access == "" {
		return string(credentials.AccessRead)
	}
	return string(row.Access)
}

func spaceOwner(row SpaceRow) string {
	if row.Owner == "" {
		return "a team"
	}
	return row.Owner
}

func headerAccount(header SpaceHeader) string {
	if header.Account == "" {
		return "—"
	}
	return header.Account
}

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

package app

// The scoped settings of one process: which directory remembers which
// target, how that record and the one global S3 default are read, and when a
// successful pull or commit stores the target it proved
// (architecture/config.md, The remembered target of a directory). A
// path-taking command always consults them; serve consults them only for a
// durable directory, because a scratch directory is process-owned.

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/settings"
)

// settingsAssociation is one process's association with the scoped settings:
// the notebook path whose record is read, empty when the command gave none or
// when serve serves its workspace root, the reader behind it, and the writer
// that stores a record a successful operation proved. A nil load is a process
// that consults no association at all.
type settingsAssociation struct {
	path   string
	load   func() (settings.Set, error)
	record func(context.Context, settings.Entry) error
}

// settingsView is what one directory's scoped settings say: the target it
// remembers, when it has one, and the one global S3 default. A process that
// consults no association sees the empty view.
type settingsView struct {
	record       settings.Target
	recorded     bool
	s3Default    settings.Target
	defaultFound bool
}

// key is the directory whose record this process reads: the notebook path the
// command resolved, or the workspace root when it gave none, because an
// omitted path is that root.
func (a settingsAssociation) key(workspaceRoot string) string {
	if a.path == "" {
		return workspaceRoot
	}
	return a.path
}

// read is the record and the global default of this directory, keyed by the
// notebook path the command resolved or, for a bare command and serve, by the
// workspace root, whose notebook directory is that root. The key is
// byte-bound, so two spellings of one directory are two records, and each is
// read by its own key. A directory with no record has none, and a file that
// cannot be read is a refusal naming it, never an empty answer: a silent
// fallback would pull another notebook and look like lost notes.
func (a settingsAssociation) read(workspaceRoot string) (settingsView, error) {
	if a.load == nil {
		return settingsView{}, nil
	}
	set, err := a.load()
	if err != nil {
		return settingsView{}, unusableSettings(err)
	}
	view := settingsView{}
	if target, ok := set.Lookup(a.key(workspaceRoot)); ok {
		view.record, view.recorded = target, true
	}
	view.s3Default, view.defaultFound = set.S3Default()
	return view, nil
}

// rememberSpace stores the notebook a successful pull or commit proved the
// directory holds, so a later bare command in it reaches the same target with
// no flag. A failed operation proves nothing and records nothing, and a write
// that fails warns and changes no result: the record is a convenience beside
// the operation, never a part of it (D8, F23, F24).
func (r *Runtime) rememberSpace(ctx context.Context, err error) {
	if err != nil {
		return
	}
	entry, ok := r.recordEntry()
	if !ok {
		return
	}
	if storeErr := r.p.association.record(ctx, entry); storeErr != nil {
		notebook.LoggerFrom(ctx).Warn("remembering the notebook failed", "path", entry.Path, "error", storeErr.Error())
	}
}

// recordEntry is the entry a successful operation stores, and whether it
// stores one at all. A record is written only where the directory remembers
// nothing and a flag or a slivingdoc variable named the target: the login's
// default space, the global S3 default and a variable's token are answers for
// this run alone, and a record beside a flag is what the directory already
// holds rather than a choice this run may overwrite (I3, I6, I7). A scratch
// server owns no directory that survives it, so it records nothing (I8).
func (r *Runtime) recordEntry() (settings.Entry, bool) {
	switch {
	case r.p.association.record == nil, r.cfg.sessionDir != "", r.cfg.recorded:
		return settings.Entry{}, false
	case r.cfg.token != "":
		return settings.Entry{}, false
	case r.cfg.bucketFrom.kind() == kindOther, r.cfg.bucket == "":
		return settings.Entry{}, false
	}
	return settings.Entry{
		Path:   r.p.association.key(r.cfg.workspaceRoot),
		Target: r.cfg.target(),
	}, true
}

// target is the complete target this process resolved, in the shape the
// scoped settings store: a hosted target names no endpoint, because the
// selected login supplies the site, while an S3 target names the effective
// endpoint, region, profile, and path style beside its bucket and prefix.
func (c config) target() settings.Target {
	if c.hosted() {
		return settings.Target{Kind: settings.KindHosted, Name: c.bucket, Prefix: c.prefix}
	}
	return settings.Target{
		Kind: settings.KindS3, Endpoint: c.endpoint, Region: c.region, Profile: c.profile,
		Name: c.bucket, Prefix: c.prefix, PathStyle: c.pathStyle,
	}
}

// rememberingService is the MCP server's notebook view with the record beside
// it: a successful tool call stores the target it proved, under the same
// rules as the one-shot commands. A failed call and a failed write change no
// result (I3).
type rememberingService struct{ rt *Runtime }

func (s rememberingService) Root() string            { return s.rt.svc.Root() }
func (s rememberingService) ReadOnlyPaths() []string { return s.rt.svc.ReadOnlyPaths() }
func (s rememberingService) WritablePaths() []string { return s.rt.svc.WritablePaths() }
func (s rememberingService) Pull(ctx context.Context) (notebook.Result, error) {
	res, err := s.rt.svc.Pull(ctx)
	s.rt.rememberSpace(ctx, err)
	return res, err
}

func (s rememberingService) Commit(ctx context.Context, message string) (notebook.Result, error) {
	res, err := s.rt.svc.Commit(ctx, message)
	s.rt.rememberSpace(ctx, err)
	return res, err
}

// WithAssociation returns the options with the association of the notebook
// directory path, or of the workspace root when path is empty: the reader of
// the scoped settings, whose records are keyed by that path. The pull, commit,
// status and log commands set it, and so does serve, whose one notebook
// directory is its workspace root; a scratch serve consults none, and the
// resolution skips the association when it owns a session directory
// (architecture/config.md).
//
// The file is located by the configuration directory rule and read once per
// process, so every resolution of the process is over the same set. A machine
// without a configuration directory has no record, which reads as an empty
// set: a missing file is how the feature is off.
func (o ProcessOptions) WithAssociation(path string) ProcessOptions {
	o.NotebookPath, o.Load, o.Record = path, scopedSettings(o.Env), rememberSettings(o.Env)
	return o
}

// association is this process's association, or the nil association of a
// process that consults none.
func (o ProcessOptions) association() settingsAssociation {
	return settingsAssociation{path: o.NotebookPath, load: o.Load, record: o.Record}
}

// scopedSettings is the reader of the scoped settings file of an environment:
// it locates the file once and reads it once, and answers every later call
// with that same set.
func scopedSettings(environment []string) func() (settings.Set, error) {
	getenv := EnvLookup(environment)
	var (
		once sync.Once
		file settings.File
		set  settings.Set
		err  error
	)
	return func() (settings.Set, error) {
		once.Do(func() {
			// A machine without a configuration directory has no record,
			// and no record is not a failure.
			if file, err = settings.Locate(getenv, runtime.GOOS); err != nil {
				err = nil
				return
			}
			set, err = file.Load()
		})
		return set, err
	}
}

// rememberSettings is the writer of one directory's record: it locates the
// file by the configuration directory rule and stores the entry under the
// settings lock, the way internal/credentials stores a login, so that two
// directories recorded in turn keep one another's entries.
func rememberSettings(environment []string) func(context.Context, settings.Entry) error {
	getenv := EnvLookup(environment)
	return func(ctx context.Context, entry settings.Entry) error {
		file, err := settings.Locate(getenv, runtime.GOOS)
		if err != nil {
			// A machine without a configuration directory has nowhere to
			// remember, which is no more a failure than having no record to
			// read.
			return nil
		}
		return storeEntry(ctx, file, entry)
	}
}

// storeEntry records one directory's notebook as a read-modify-write under
// the settings lock. An entry that already says this is left byte for byte,
// so a repeated operation never rewrites the file, and every other entry
// survives the replacement of one.
func storeEntry(ctx context.Context, file settings.File, entry settings.Entry) (errOut error) {
	lock, err := file.Lock(ctx)
	if err != nil {
		return err
	}
	defer func() {
		if unlockErr := lock.Unlock(); errOut == nil && unlockErr != nil {
			errOut = unlockErr
		}
	}()
	set, err := file.Load()
	if err != nil {
		return err
	}
	if recorded, ok := set.Lookup(entry.Path); ok && recorded == entry.Target {
		return nil
	}
	return file.Save(set.Put(entry))
}

// unusableSettings is the refusal of a settings file this build cannot read.
// The file's own refusal names it and how to fix it; this names the choice
// that keeps the command running without it.
func unusableSettings(err error) error {
	return fmt.Errorf("%w; remove it to choose the space with --space, --storage s3 or SLIVINGDOC_SPACE", err)
}

// noCredentialRefusal is the refusal for a remembered space that no
// credential reaches: it names the space, and the endpoint mismatch when that
// is why no stored login applies, and it never falls back to S3
// (architecture/login.md).
func noCredentialRefusal(logins credentials.Set, explicit, space string) error {
	msg := fmt.Sprintf("this directory remembers hosted space %q, which no stored login or SLIVINGDOC_TOKEN reaches", space)
	if others := logins.Logins(); explicit != "" && len(others) > 0 {
		msg += fmt.Sprintf(" at %s; the stored login is for %s", explicit, others[0].Endpoint)
	}
	return fmt.Errorf("%s; run 'slivingdoc login', or pass --space to name another space", msg)
}

// prefixMismatch is the refusal for a prefix a flag or the environment named
// beside a different remembered prefix. The record says which notebook the
// directory holds, so a named prefix that differs would address another one in
// the same store (architecture/config.md).
func prefixMismatch(kind settings.Kind, name, named, remembered string) error {
	target := "hosted space"
	if kind == settings.KindS3 {
		target = "bucket"
	}
	return fmt.Errorf("this directory remembers %s %q with prefix %q, and %q names another notebook in it; pass --prefix %s, or use another directory",
		target, name, remembered, named, remembered)
}

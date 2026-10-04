package app

// The scoped settings of one process: which directory remembers which hosted
// notebook, how that record is read, and when a successful pull or commit of
// a path-taking command stores it (architecture/config.md, The remembered
// notebook of a directory). Only a path-taking command consults the record,
// and only where the flags and the environment named no space.

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/settings"
)

// settingsAssociation is one process's association with the scoped settings:
// the notebook path whose record is read, empty when the command gave none,
// the reader behind it, and the writer that stores a record a successful
// operation proved. A nil load is a process that consults no association at
// all, which is what serve is.
type settingsAssociation struct {
	path   string
	load   func() (settings.Set, error)
	record func(context.Context, settings.Entry) error
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

// read is the notebook this directory remembers, keyed by the notebook path
// the command resolved or, for a bare command, by the workspace root, whose
// notebook directory is that root. The key is byte-bound, so two spellings of
// one directory are two records, and each is read by its own key. A
// directory with no record has none, and a file that cannot be read is a
// refusal naming it, never an empty answer: a silent fallback would pull
// another notebook and look like lost notes.
func (a settingsAssociation) read(workspaceRoot string) (settings.Target, error) {
	if a.load == nil {
		return settings.Target{}, nil
	}
	set, err := a.load()
	if err != nil {
		return settings.Target{}, unusableSettings(err)
	}
	target, _ := set.Lookup(a.key(workspaceRoot))
	return target, nil
}

// rememberSpace stores the notebook a successful pull or commit proved the
// directory holds, so a later bare command in it reaches the same space with
// no flag. A failed operation proves nothing and records nothing, and a write
// that fails warns and changes no result: the record is a convenience beside
// the operation, never a part of it (D8, F23, F24).
func (r *Runtime) rememberSpace(ctx context.Context, err error) {
	if err != nil || !r.recordsSpace() {
		return
	}
	// The key is the one the record is read by, so a write and the next
	// read of this directory always agree.
	entry := settings.Entry{
		Path:   r.p.association.key(r.cfg.workspaceRoot),
		Target: settings.Target{Space: r.cfg.bucket, Prefix: r.cfg.prefix},
	}
	if storeErr := r.p.association.record(ctx, entry); storeErr != nil {
		Module(r.base, ModuleNotebook).Warn("remembering the hosted notebook failed", "path", entry.Path, "error", storeErr.Error())
	}
}

// recordsSpace says whether this process records the space it resolved:

//   - the command takes a notebook path, so the process carries an
//     association at all; serve carries none and records nothing,
//   - the store is hosted and the space is non-empty, because a bucket is
//     not a space,
//   - the space came from a flag, a variable or the record itself, and
//     never from the login's default space, which must
//     stay free to change everywhere, nor from the token's own space, which
//     names its space for this run only and ignores the record (D11),
//   - and an explicit choice records only where the directory remembers
//     nothing: what a directory remembers is the notebook it already
//     holds, and a flag or a variable beside that record is a choice for
//     this run alone, which the next bare command must not inherit
//     (F15, F16).
func (r *Runtime) recordsSpace() bool {
	switch {
	case r.p.association.record == nil, !r.cfg.hosted(), r.cfg.bucket == "":
		return false
	case r.cfg.bucketFrom.kind() != kindOther:
		return r.cfg.remembered.Space == ""
	default:
		return r.cfg.bucketFrom == bucketFromRemembered
	}
}

// WithAssociation returns the options with the association of the notebook
// directory path, or of the workspace root when path is empty: the reader of
// the scoped settings, whose records are keyed by that path. The pull, commit,
// status and log commands set it, so a directory that pulled a hosted notebook
// selects its own space with no flag; serve leaves it unset, because a running
// server's store is fixed before any notebook directory is known
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
// the same space (architecture/config.md).
func prefixMismatch(space, named, remembered string) error {
	return fmt.Errorf("this directory remembers hosted space %q with prefix %q, and %q names another notebook in it; pass --prefix %s, or use another directory",
		space, remembered, named, remembered)
}

# Phase 1: the scoped settings store

**Status:** Complete

README: [README.md](./README.md)

## Goal

Build `internal/settings`, the one versioned file that maps a directory to the
hosted notebook it belongs to, with the same hardening the credentials file has.

## Specification

### The package

`internal/settings` owns exactly one stored artifact and never sends anything
anywhere. Its interface is the one the README fixes: `FormatVersion`,
`FileName`, `LockName`, `Target`, `Entry`, `Set`, `Config`, `Locate`, `File`
with `Path`, `Load`, `Save` and `CheckDir`, and `Set.Lookup` and `Set.Put`.
`Put` is pure and returns a new `Set`; the read-modify-write belongs to the
caller so the lock covers it.

```text
internal/settings/
  settings.go      package doc; Config, Load, Save
  locate.go        Locate, the configuration directory and its owner checks
  codec.go         encode, decode, strictjson shape, the version error
  settings_test.go every unit test named in the tables below
```

The package depends on `internal/strictjson` and `github.com/gofrs/flock`, and
on nothing else from `internal/`.

### The stored shape

One object, decoded with `strictjson`, so an unknown, duplicate, missing or
null field is refused exactly as `state.json` and the manifest are. Fields in
order: `version` (the package's `FormatVersion`), and `entries`, an array whose
each element carries `path` and a `target` object with `space` and `prefix`.
`Set` keeps the entries in the order the file lists them, and `Put` replaces an
entry whose `path` is byte-identical and appends a new one, so a rewrite of an
existing key does not move it and another writer's entry survives.

The encoded form is compact, with HTML escaping off and no trailing newline,
the same as `internal/workspace`'s `state.json`.

### The file and its directory

`Locate` resolves the directory with the same rule and the same environment
variable as `credentials.Locate`, including its refusal when neither
`SLIVINGDOC_CONFIG_DIR` nor the platform directory resolves from the injected
environment. To keep one convention rather than two, Phase 1 **exports** the
credentials package's directory resolution instead of copying it, and updates
[`architecture/login.md`](../../architecture/login.md) in the same commit. The
new export returns the resolved directory and the effective user id, so
`settings.Locate` reuses the rule and keeps its own `File` type. `settings` has
no `platform_*.go` files of its own: the credentials package already owns the
POSIX owner and mode checks, and Phase 1 exports the two helpers it needs
rather than reimplementing them.

| Bound | Value | Enforced by |
| --- | --- | --- |
| File read | `Config.MaxFileSize`, or the package bound when zero | `Load`, before decoding |
| File write | the same value | `Save`, before writing the temp file |
| File mode | the credentials file's own mode | `Save` on the temp file |
| Directory mode | the credentials file's own mode | `Save` and `File.Lock` on `MkdirAll` |
| Lock retry interval | the credentials file's own interval | `File.Lock` |

### Hardening

`Load` mirrors the credentials file exactly: a missing file is an empty `Set`,
a symbolic link is refused, the file is opened without following a final link
where the platform can and checked through the open descriptor, the directory
is checked for ownership and mode, the file for type, ownership and mode, and
the read is bounded. `Save` writes a temporary file in the same directory,
syncs it, and renames it over the file. `File.Lock` creates the directory when
missing and takes the lock beside the file.

The settings lock is a **separate file** from the credentials lock, so the two
never deadlock each other: nothing takes both at once.

### Errors

The package returns typed errors mirroring the credentials ones, with the
package name leading: no configuration directory, a malformed file, a file of
another version, a file that is not private, and a file at or over the bound.
The version error's wording follows the credentials precedent exactly: an
older file is removed by the person, a newer build's file is kept and
slivingdoc is updated, and the file is never rewritten.

| Error | Wraps | Text names |
| --- | --- | --- |
| `ErrNoConfigDir` | nothing | `SLIVINGDOC_CONFIG_DIR` and `HOME` |
| `ErrMalformed` | the strictjson error | the file path |
| `ErrUnsupportedVersion` | nothing | the file path, and the two fixes |
| `ErrExposed` | nothing | the file or directory path and the `chmod` that fixes it |
| `ErrTooLarge` | nothing | the file path and the bound |

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `Locate` with `SLIVINGDOC_CONFIG_DIR` | injected environment | that directory | none | reading or creating anything |
| `Locate` with nothing set | injected environment, goos | the platform directory | none | reading the developer's own file |
| `File.Load` with no file | `os.Lstat` | an empty `Set` | none | an error |
| `File.Load` with a valid file | `strictjson` | the entries in file order | none | writing, following a link |
| `File.Load` with a token field | `strictjson` | `ErrMalformed` | none | writing |
| `File.Save` with a new entry | `flock`, temp file, rename | the file now holds both entries | directory created at the directory mode | a partial file left behind |
| Two processes saving | `flock` | both entries present | none | a lost entry |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| `Load` on a valid file round-trips through `Save` | `TestLoadSaveRoundTrip` |
| An unknown field, a duplicate, a missing and a null field are refused | `TestDecodeRejectsStrictViolations` |
| A `target` carrying a token or a key is refused | `TestDecodeRejectsCredentialField` |
| Another version is refused and the file is left byte-identical | `TestUnsupportedVersionIsRefusedUnwritten` |
| A symbolic link, a directory and a device are refused | `TestLoadRefusesWhatIsNotTheFile` |
| An exposed file and an exposed directory are refused | `TestLoadRefusesExposedFileAndDir` |
| A file one byte over the bound is refused on load and on write | `TestBoundRefusesOverSizeFile` |
| `Put` replaces an existing key in place and appends a new one | `TestPutReplacesAndAppends` |
| `Locate` follows `credentials.Locate` for both the variable and the platform | `TestLocateMatchesCredentialsLocate` |
| Two processes writing two entries both persist | `TestConcurrentSavesKeepBothEntries` |
| A saved file has the documented mode and its directory the documented mode | `TestSaveModes` |
| `Config.Load` is used instead of the file when injected | `TestConfigInjectedLoadSkipsTheFile` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| No configuration directory | `ErrNoConfigDir` | `TestLocateWithoutConfigDir` |
| Malformed JSON | `ErrMalformed` | `TestDecodeRejectsMalformedJSON` |
| Unknown version | `ErrUnsupportedVersion`, file untouched | `TestUnsupportedVersionIsRefusedUnwritten` |
| Symbolic link | `ErrExposed` | `TestLoadRefusesWhatIsNotTheFile` |
| Group-readable file | `ErrExposed` | `TestLoadRefusesExposedFileAndDir` |
| Oversized file | `ErrTooLarge` | `TestBoundRefusesOverSizeFile` |
| Lock held by another process | an error naming the lock | `TestSaveRefusesWhileLocked` |
| Rename fails | the temp file is removed and the error is returned | `TestSaveRemovesTempFileOnFailure` |

## Implementation notes

### 2026-10-03: phase 1, worker session (agent, clai)

**Deviation 1: the shared handling is one exported type, not exported
helpers.** The phase planned to export the credentials package's owner,
mode, lock and directory helpers. Copying them into
`internal/settings` would have produced two nearly identical sets of
filesystem code, so the helpers moved into `credentials.PrivateFile`
(`privatefile.go`), which every configuration file goes through. The
schema and the wording stay in each package: a `Wording` value names the
message prefix, the not-private error, the over-bound error and the
commands that hold the lock. `File.hardened` builds the credentials value,
and `File.hardened` in `internal/settings` builds its own.

**Deviation 2: `ConfigDir` and `NoConfigDir` replace two unexported
functions.** `credentials.ConfigDir(getenv, goos)` answers the
configuration directory and the effective user, and a failure is a
`credentials.NoConfigDir` naming the cause. `credentials.NoDirectory`
words such a failure in the caller's own sentinel, so
`credentials.ErrNoConfigDir` and `settings.ErrNoConfigDir` keep the
message each package's callers already read. `userConfigDir` and
`checksOwners` stayed unexported behind them; `checksOwners` became
`ChecksOwners` because a package outside the one locating a file needs it.
`maxFileSize` became `credentials.MaxFileSize`, because the settings
bound is that bound.

**Deviation 3: `File` carries the bound, and `Config` reads through
`Locate`.** The README's interface shows `Config.MaxFileSize` and a
`Config.Load func() (Set, error)`. The bound has to reach the read, so it
is a field of `File` too (`File.withBound`), and `Config.Read(getenv,
goos)` takes the same environment and operating system as
`credentials.Locate`, because a caller that reads the store has to
locate it the same way. `Set.Entries` is exported so a test, and Phase
3, can inspect what a set holds without a file.

**Deviation 4: the lock refusal never names a holder.** `flock`
returns the context error when the context ends first, so the "another
command holds it" wording is only reachable when the lock path itself
fails; `TestSaveRefusesWhileLocked` therefore proves the deadline, and
the holder wording is exercised by `credentials`' own lock rows.

**Deviation 5: the device row is a FIFO.** A device node needs a
privilege no test has, so `TestLoadRefusesWhatIsNotTheFile` covers a
symbolic link (to a stored file and dangling), a directory and a FIFO,
which all reach the same "not a regular file" refusal through build-tagged
`mkfifo` helpers exactly as `internal/credentials` does.

**Commands run, and their results.**

```bash
go test ./internal/credentials/... ./internal/strictjson/...   # before: ok, ok
make test    # ok: every package, coverage 88.8% (floor 70%)
make lint    # gofumpt, go vet, staticcheck, go fix: clean
make npm-test  # ok
go run github.com/mibk/dupl@v1.0.0 -t 80 .   # 2 pre-existing clone groups, none in this phase
go test -count=1 -coverpkg=./internal/settings ./internal/settings/   # coverage 97.7%
go test -race -count=3 -timeout=30s ./internal/settings/   # ok
```

`make test`, `make lint` and `make npm-test` were each run twice: once
after the store was written, and once after the tests were finished. Both
runs of each passed. No test was skipped except on Windows, where POSIX
owners, permission bits and symbolic links do not exist.

**Surprise worth a note.** `go test -coverprofile` with a multi-package
`-coverpkg` printed `total 0.0%` in `go tool cover -func` while the run
itself reported real percentages; one package at a time in `-coverpkg`
is the reliable form.

## Review findings

### Review 2 — 2026-10-04 — status `Complete` (one nit)

**R2-05 — nit — `internal/credentials/platform_other.go:22`.** The comment on
`fileOwner` and `pathOwner` names `checksOwners`, but the exported function is
`ChecksOwners` (`internal/credentials/privatefile.go`). The build is unchanged;
the comment sends the next reader to a name that does not exist.

- [x] Correct the name in the comment. Closed by Phase 6.

### Verified good (review 2)

The review read `internal/credentials/privatefile.go` and `internal/settings/`
in full and traced the file handling through every branch: `Load` refuses a
symbolic link, anything but a regular file, an exposed file or directory and a
file over the bound; `Save` locks beside the file, writes a 0600 temporary file
and renames it; the codec refuses an unknown, duplicate, missing and null field
(including a token or a key) and refuses another version before any decode,
without rewriting the file. The concurrency test proves two processes keep both
entries. `Locate` answers without touching the credentials file, and the bound
travels on `File` so `Config.MaxFileSize` reaches the read.
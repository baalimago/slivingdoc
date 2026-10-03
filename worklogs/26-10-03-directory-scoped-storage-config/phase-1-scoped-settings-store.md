# Phase 1: the scoped settings store

**Status:** Not Started

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

Not started.

## Review findings

None.
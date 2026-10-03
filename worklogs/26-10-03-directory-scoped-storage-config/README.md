# slivingdoc directory-scoped storage configuration worklog

**Status:** README drafted, awaiting sign-off. Every phase is `Not Started` and
no phase file exists yet.

**Architecture:**
[`../../architecture/config.md`](../../architecture/config.md),
[`../../architecture/login.md`](../../architecture/login.md),
[`../../architecture/workspace.md`](../../architecture/workspace.md),
[`../../architecture/decisions.md`](../../architecture/decisions.md)

## Goal

Let a person keep notes from several hosted spaces in sibling directories and
work in each one without repeating `--space` and `--storage hosted`. A directory
that has been pulled remembers the hosted space it belongs to, and a later
`pull`, `commit`, `status` or `log` there selects that space by itself. The
association lives in the user's configuration directory, never inside the
notebook, and it stores no credential. The store is shaped so further
per-directory settings can be added beside the space without redesigning it.

## Status board

| Phase | Status | Outcome |
| --- | --- | --- |
| [1. Scoped settings store](phase-1-scoped-settings-store.md) | Not Started | A versioned, strict, locked settings file under the user configuration directory maps a canonical directory path to its settings, with the hardened file handling the credentials file already uses |
| [2. Space resolution and precedence](phase-2-space-resolution.md) | Not Started | Flags, then environment, then the directory's remembered space, then the account default, with a named refusal for every way a remembered space can fail |
| [3. Recording the association](phase-3-recording-the-association.md) | Not Started | A successful pull or commit records the resolved hosted space for that directory; a failed operation records nothing; a write failure warns and changes no result |
| [4. Operator surface and docs](phase-4-operator-surface.md) | Not Started | `status` names the source of its space, the help and operator docs describe the order, and a new recorded decision exists |
| [5. Quality gate](phase-5-quality-gate.md) | Not Started | The full gate is green with the new scenarios, and the readiness checklist is re-verified |

Phases complete in numeric order. Phase 2 depends on Phase 1 (it reads the
store Phase 1 builds). Phase 3 depends on Phase 2 (it writes what Phase 2
resolved). Phase 4 depends on Phases 2 and 3 (it reports what they resolve and
record). Phase 5 depends on all of them.

There is no gating phase 0. Every claim below was read in the code named next
to it. The one cost this effort adds is reading one small file before startup
resolution, and it is bounded by the same file-size bound the credentials file
already accepts, so it needs no measurement phase.

An executing agent reads this README and only its phase file. Anything two
phases share is written here.

## Strategy

### Evidence the design rests on

- **The private workspace directory cannot hold the association.**
  `DerivedKey` hashes the canonical path *together with* the storage identity
  ([`internal/workspace/identity.go`](../../internal/workspace/identity.go),
  [workspace.md](../../architecture/workspace.md)), so reading an entry from
  `<private-root>/<key>/` requires already knowing the identity it is keyed by.
  The association must be reachable by path alone, which is what a settings
  file keyed by the canonical path provides.
- **A config file inside the notebook would become content.** Every visible
  text file that no ignore rule names is notebook state and is published by the
  next commit
  ([workspace.md](../../architecture/workspace.md#notebook-content-rules)). A
  per-directory file would also be scanned, so the association would be pulled,
  committed and stored in the very space it names.
- **The directory is known before storage is configured, but not always.**
  `cmd/pull`, `cmd/commit`, `cmd/status` and `cmd/log` call `app.OperationPath`
  before `app.Setup` ([`cmd/pull/pull.go`](../../cmd/pull/pull.go),
  [`cmd/commit/commit.go`](../../cmd/commit/commit.go)), yet an omitted path
  resolves to the workspace root only inside `Runtime.resolve`
  ([`internal/app/app.go`](../../internal/app/app.go)), which runs after
  startup. The key is therefore the **workspace root** when no path argument is
  given and the cleaned absolute argument when one is.
- **The space is chosen in `resolveStorage`, but the space the process really
  uses is only known after `buildService`**
  ([`internal/app/app.go`](../../internal/app/app.go)): a stored login's token
  is minted there, and `resolveHostedSpace` asks `GET /v1/token` for the
  token's space. The read therefore enters `resolveStorage`, where the flags
  and the environment are already resolved, and the recording happens in
  `Runtime` after an operation returned.
- **`s3Signals` decides whether this feature is usable at all.** Under `auto`, a
  space that did not come from the login's default, beside any S3 signal, is a
  refusal ([`internal/app/storage.go`](../../internal/app/storage.go)), and an
  existing `~/.aws/credentials` is such a signal. A remembered space is by
  construction not the default, so unless it counts like the default for that
  check the feature refuses on any machine with AWS files present.
- **The settings file needs the credentials file's hardening, which is
  unexported.** `internal/credentials` locates the configuration directory,
  bounds the file size, refuses exposed permissions, locks a read-modify-write
  and replaces atomically
  ([`internal/credentials/credentials.go`](../../internal/credentials/credentials.go)),
  but every helper is unexported, so sharing means exporting it and updating
  [login.md](../../architecture/login.md) in the same change.
- **`guardFirstPull` is weaker than "a directory cannot switch spaces".** It
  returns immediately when the workspace is already pulled, and an **empty**
  remote admits any local file to seed the notebook
  ([`internal/notebook/pull.go`](../../internal/notebook/pull.go)). A populated
  directory can therefore be re-pointed at an empty space, so recording after
  a successful operation is a real re-point and needs its own rule.
- **Commit cannot precede a pull.** `notes_commit` requires the `pulled`
  marker, so a directory that can commit has already pulled
  ([workspace.md](../../architecture/workspace.md#private-directory-layout)).
  Commit-side recording is therefore a repair path for an entry lost with the
  file, not a second way to configure a directory.
- **The token reaches one space, so a mismatch is a refusal, not a choice.**
  With `SLIVINGDOC_TOKEN` the process takes the space from `GET /v1/token`, and
  a named space that differs refuses startup, worded for the setting that named
  it ([`internal/app/app.go`](../../internal/app/app.go)). A remembered space
  must never be worded as a flag the operator passed.
- **The identity already separates same-named spaces of two accounts.**
  `Identity.SpaceID` hashes into `DerivedKey` and `SharedCacheDirName`
  ([`internal/workspace/identity.go`](../../internal/workspace/identity.go)),
  so private state and the pack cache stay apart. What a space name alone does
  not separate is which stored **login** is used, which is why the endpoint is
  not remembered (D9).

### Invariants that bind several actors

Every row is asserted by the test in its last column. A path that is not a row
does not exist; a phase that needs one adds a row first.

| Invariant | Actors | Mechanism | Test |
| --- | --- | --- | --- |
| The settings file holds no credential | Phase 1 store, Phase 2 read, Phase 3 write, Phase 4 report | The record type has no token or key field, and the store decodes a strict schema that rejects unknown fields | `internal/settings`: decode rejects a token field; the scenario's recorded file holds only the version, the path and the space |
| The association never overrides an explicit choice | Phase 2, Phase 3 | Resolution consults the store only after flags and environment resolved nothing, and Phase 3 writes only what resolution produced | Scenario: a remembered space plus `--space` uses the flag and leaves the entry byte-identical |
| A remembered space selects hosted mode | Phase 2 | The space source carries the same host-only status as the login's default, so `s3Signals` never turns it into an ambiguity refusal | Scenario: a remembered space beside an existing `~/.aws/credentials` reaches the hosted space |
| A remembered space is never a silent S3 fallback | Phase 2 | The store is consulted before the auto mode decides S3 for want of a login, and an unreadable store is a refusal rather than an empty result | Scenario: a remembered space with no credential and no login refuses and reaches no bucket |
| `serve` neither reads nor writes the association | Phase 2, Phase 3 | Only a path-taking command's resolution reads the store, and only a successful `pull` or `commit` of such a command writes it | Scenario: the recorded file is byte-identical after a `serve` in a directory that has an entry |
| A failed operation records nothing | Phase 3 | The record happens after the operation returned no error, on the result value the CLI report uses | Scenario: a refused pull and a conflicted pull leave no entry |
| An existing entry is replaced only by a successful operation against the space it names | Phase 3 | The write replaces one entry of the whole set under the lock, and the write path is reached only with the resolved space in hand | Scenario: a second directory's successful pull replaces only its own entry |
| A write failure changes no operation result | Phase 3 | The record is best effort, logs a warning, and returns no error to the caller | Unit: the injected writer fails and the caller still returns the operation result |
| A remembered space the credentials cannot reach is a refusal, never a fallback | Phase 2 | Each unreachable case returns a typed error from resolution; no branch substitutes the account default or an S3 bucket | Scenario: a remembered space with an expired login refuses and names the space |
| Concurrent writers cannot lose an entry | Phase 1 | The lock beside the file, a whole-file read under it, then temp file and rename | Two processes writing two entries both persist |
| A settings file of another version is never rewritten | Phase 1, Phase 2 | The version check returns a typed error before any decode or write, worded like the credentials file's | Unit plus a startup-refusal scenario |

### Failure surface

Every refusal a phase can produce, with the test that proves it. Row numbers are
stable references for the phases.

| # | Situation | Behavior | Token | Test |
| --- | --- | --- | --- | --- |
| F1 | No settings file | No entry; resolution continues unchanged | none | Unit |
| F2 | No user configuration directory | Memory unavailable; resolution continues unchanged | none | Unit |
| F3 | Settings file unreadable or malformed | Refuse, naming the file and that removing it restores today's behavior | startup refusal | Scenario |
| F4 | Settings file of another version | Refuse; an older file is removed by the person, a newer build's keeps slivingdoc updated | version error | Scenario |
| F5 | Settings file or its directory exposed to other users | Refuse, worded like `credentials.ErrExposed` | startup refusal | Scenario |
| F6 | Settings file is a symbolic link or not a regular file | Refuse, worded like the credentials file's | startup refusal | Scenario |
| F7 | Remembered space fails `httpstore.ValidateSpace` | Refuse naming the space as invalid | startup refusal | Scenario |
| F8 | Remembered endpoint is not `https` and not loopback | Never reachable: no endpoint is remembered (D9) | none | not applicable |
| F9 | Remembered space, no token and no stored login | Refuse naming the space and `slivingdoc login`; never fall back | startup refusal | Scenario |
| F10 | Remembered space, the only stored login is for another endpoint | Refuse naming both endpoints, as `noLoginRefusal` does today | startup refusal | Scenario |
| F11 | Remembered space, several stored logins match the endpoint | Refuse asking for `--endpoint`, as `pickLogin` does today | startup refusal | Scenario |
| F12 | Stored login expired or revoked | Refuse telling the user to log in again | startup refusal | Scenario |
| F13 | Remembered space not in the login's account | Refuse naming the space as unreachable for this account | startup refusal | Scenario |
| F14 | `SLIVINGDOC_TOKEN` set beside an association | The token names its own space; the association is ignored for this run | `spaceMismatch` wording extended | Scenario |
| F15 | `SLIVINGDOC_SPACE` or `SLIVINGDOC_BUCKET` set | Environment wins; the entry is neither used nor rewritten | logged | Scenario |
| F16 | `--space`, `--bucket`, `--endpoint` set | Flags win; the entry is neither used nor rewritten | logged | Scenario |
| F17 | `--storage s3` or `SLIVINGDOC_STORAGE=s3` | S3 mode; the entry is neither used nor written | logged | Scenario |
| F18 | `--storage hosted` with no space | The remembered space fills it | none | Scenario |
| F19 | Remembered space beside ambient S3 signals | The remembered space wins; no ambiguity refusal | logged | Scenario |
| F20 | A resolved prefix differs from the remembered prefix | Refuse naming both, saying which one this directory belongs to; a bare pull may not silently address another notebook in the space | startup refusal | Scenario |
| F21 | Two entries for one directory under different spellings | Both are kept; each is looked up by its own key | none | Unit |
| F22 | Remembered space on a host that can no longer reach the hosted API | The hosted adapter's existing refusal, with no fallback | `ACCESS_DENIED` | Scenario |
| F23 | Recorded file write fails | Warn; the operation result and exit code are unchanged | warning | Unit |
| F24 | The recorded file would grow past its bound | The write is refused, a warning names the bound, and only this run's association is lost | warning | Unit |

### Limits and budgets

| Limit | Injectable field | Parameter | How a test triggers it |
| --- | --- | --- | --- |
| Settings file size on read and write | `Config.MaxFileSize` | `maxSettingsFileSize` | A file one byte over the bound is refused on load and refused on write |
| Remembered directories per file | none | `minSettingsEntries` | The store keeps one entry per directory and never prunes; a file at the size bound holds this many entries, so the bound is the effective limit and the write that would exceed it warns |
| Store reads per operation | `Config.Load` | injected `Load` | A test counts `Load` calls through one `pull` and asserts one |

### Shared interface between phases

Phase 1 owns the store; Phases 2 and 3 consume it and Phase 4 reports through
the accessors Phase 2 introduces. The names are fixed here so no phase invents a
second vocabulary.

```text
package settings   // internal/settings

const FormatVersion = 1
const FileName     = "workspaces.json"
const LockName     = "workspaces.lock"

// Target is the remembered hosted notebook of one directory: the space and
// the prefix that identify it inside that space. It names no credential, and
// it is deliberately not called Storage: that word belongs to
// internal/storage.
type Target struct{ Space, Prefix string }

type Entry struct{ Path string; Target Target }   // one directory
type Set struct{ entries []Entry }                // decoded file

type Config struct {
    MaxFileSize int                   // 0 means the package bound
    Load        func() (Set, error)   // injected for tests; nil reads the file
}

func Locate(lookup func(string) string, goos string) (File, error)
type File struct{ ... }               // the directory and its owner checks
func (f File) Path() string
func (f File) Load() (Set, error)     // strict, bounded, hardened
func (f File) Save(s Set) error       // locked, atomic
func (f File) CheckDir() error
func (s Set) Lookup(path string) (Target, bool)
func (s Set) Put(entry Entry) Set     // pure: returns the new set
```

The `Target` holds the space and the prefix, and names no credential. The
**endpoint is not remembered** (D9):
the endpoint of a hosted process is the login's own, and a remembered endpoint
would choose silently among several stored logins, defeating the ambiguity
refusal that F11 keeps. F10 and F11 therefore keep today's meaning for a
remembered space exactly as they do for a space named by a flag.

Phase 2 reads through `Locate`, `Load` and `Lookup`. Phase 3 writes through
`Locate`, `Load`, `Put` and `Save`, so the read-modify-write happens under the
lock the way `internal/credentials` does it.

## Parameters and owners

| Parameter | Default | Owner |
| --- | --- | --- |
| `maxSettingsFileSize` | `Config.MaxFileSize`, which Phase 1 sets from the credentials file's own bound | Phase 1 |
| Settings file mode and directory mode | the credentials file's own modes | Phase 1 |
| Lock retry interval | the credentials file's own interval | Phase 1 |
| Configuration directory | `SLIVINGDOC_CONFIG_DIR` when set, else the platform user configuration directory, exactly as `credentials.Locate` resolves it | Phase 1 |
| Canonical path rule | `resolvePath`'s rule (home expansion, absolute, cleaned); **not** `workspace.canonicalize`, which also enforces containment | Phase 1 |
| Association key | the resolved notebook path: the cleaned argument, or the workspace root when no argument is given | Phase 2 |
| Space source constant and its wording | `bucketFromRemembered`, worded "remembered space" | Phase 2 |
| Precedence position of the association | below flags and environment, above the stored default space | Phase 2 |
| Commands that read the association | `pull`, `commit`, `status`, `log` | Phase 2 |
| Operations that record it | `pull` and `commit`, on success only | Phase 3 |
| `Runtime` accessors for the resolved space and its source | one new exported pair on `Runtime` | Phase 2 introduces, Phase 4 consumes |
| Recorded endpoint | not recorded | Phase 3, deliberate |
| Recorded prefix | the resolved prefix, recorded beside the space | Phase 3 |
| Resolved prefix differing from the recorded one | a refusal naming both (F20) | Phase 2 |
| Report line naming the association | none | Phase 4, deliberate: `status` already prints the space through `Runtime.Target()` |
| Space printed by `log` | none; `ReportLog` prints no target | Phase 4, deliberate |

## Readiness checklist

1. No numerals carrying a unit in phase files outside oracle rows. Verify with
   `grep -nE '(^|[^=])\b[0-9]+([.,][0-9]+)? ?(s|ms|MB|%)\b' phase-*.md`.
   Bare counts and ratios are oracle data for this repository and are allowed;
   the rule bounds durations, sizes and ratios.
2. Every test name is declared in exactly one phase and one file list.
3. Every config field, flag and injectable field has one owner in the
   parameters table.
4. Every invariant and limit is a table with a test per row.
5. Every phase mentioning listening, manual or paid has `Human required`.
6. No phase references text scheduled for deletion.
7. New conventions do not contradict existing code conventions: file
   handling cites `internal/credentials/credentials.go`, path handling cites
   `internal/app/command.go`, and the strict JSON shape cites
   `internal/strictjson`.

The author ran the checklist before requesting validation. Findings and the
resulting edits are in the session journal below, and Phase 5 re-runs it
against the tree as it stands.

## Decisions log

| ID | Date | Decision | Rationale | Replaces |
| --- | --- | --- | --- | --- |
| D1 | 2026-10-03 | The association is a per-user settings file under the configuration directory, keyed by canonical path | A file inside the notebook becomes published content; the private workspace directory is keyed by the identity it must supply | — |
| D2 | 2026-10-03 | Only `pull`, `commit`, `status` and `log` read the association | A running server's store is fixed at startup; a tool call must not change it | — |
| D3 | 2026-10-03 | Precedence is flags, environment, association, account default | Makes `cd` into a directory sufficient without making a global default win over a specific one | — |
| D4 | 2026-10-03 | A successful `pull` or `commit` records the resolved hosted space; a failure records nothing | Commit cannot precede a pull, so the record is written by the operation that already proved the directory belongs to the space | — |
| D5 | 2026-10-03 | A remembered space the credentials cannot reach is a refusal naming the space; no fallback | A silent fallback would pull a different notebook and look like lost notes | — |
| D6 | 2026-10-03 | The record stores the space and the prefix, and no credential | The token is environment-only and the key stays in the credentials file; the store must hold no credential | — |
| D7 | 2026-10-03 | The store is a generic scoped-settings registry, one record per directory | Leaves room for further per-directory settings without a redesign | — |
| D8 | 2026-10-03 | A write failure warns and changes no operation result | The association is a convenience; losing it must not fail an operation that already succeeded | — |
| D9 | 2026-10-03 | Only the space is remembered, never the endpoint | The endpoint of a hosted process is the login's own; remembering it would choose silently among several stored logins | — |
| D10 | 2026-10-03 | A remembered space selects hosted mode and counts like the login's default for `s3Signals` | Otherwise the feature refuses on any machine with AWS files, and a logged-out machine silently pulls S3 | — |
| D11 | 2026-10-03 | `SLIVINGDOC_TOKEN` ignores the association for that run | A token names exactly one space; the operator supplied it deliberately | — |
| D12 | 2026-10-03 | The record holds the prefix, and a resolved prefix that differs from it is a refusal | Without the prefix a bare pull would silently address a different notebook in the same space, and the mismatch could not be detected at all | — |
| D13 | 2026-10-03 | A remembered space re-points a directory only when the pull itself succeeded against it | `guardFirstPull` admits any local file against an empty remote, so the guard is not by itself proof | — |

## Definition of success

| Criterion | Evidence |
| --- | --- |
| After one `pull --space` in a directory, a bare `slivingdoc pull`, `commit`, `status` and `log` in that directory all reach the same space with no space or storage flag | The black-box scenario runs the sequence against the reference gateway |
| A sibling directory with its own space is unaffected and unreachable from the first | The same scenario, second directory |
| Flags and environment win over the association and leave the entry unchanged | Precedence rows of the same scenario |
| Every row of the failure surface above is refused with a message naming what is wrong and what to do | One refusal scenario per distinct refusal |
| No credential appears in the settings file or in any operator output | The decode test and the scenario's file inspection |
| `serve` with an entry for its root behaves exactly as before and writes nothing | The serve scenario |
| The architecture docs and a new recorded decision describe the order and the store | The documentation phase's own diff |
| The full gate is green at or above the coverage floor | `make qa` |

## Validation policy

The repository's own gate is the only gate: `make qa`, which runs `lint`,
`test` and `npm-test`. `dupl -t 80` is run separately as a signal, never as a
gate, because it has no Makefile target. No test is skipped, no build tag or
environment variable narrows the suite, and the timeout, count and race flags
are unchanged. Black-box scenarios in `internal/integrationtest` carry the
behavioral contract; unit tests cover the store's file handling and the
wording of each refusal, which no scenario reaches cheaply.

## Feedback index

| Finding | Closed by |
| --- | --- |
| Review 1, B1: the association key was unavailable for a bare command | The evidence section and the association-key parameter row: the key is the workspace root when no argument is given |
| Review 1, B2: the association never selected hosted mode | D10 plus the invariant row "A remembered space is never a silent S3 fallback" |
| Review 1, B3: `s3Signals` would refuse on any machine with AWS files | D10, the invariant row "A remembered space selects hosted mode", and F19 |
| Review 1, B4: the record's endpoint contradicted itself | D9, the interface section, and the removed F8 |
| Review 1, B5: recording was a silent re-point | D13, the invariant row on entry replacement, and the corrected evidence bullet |
| Review 1, B6: the README referenced an error table it did not contain | The failure surface table F1 to F24 |
| Review 1, M1 and M2: the interface could not reach the file; the credentials helpers are unexported | The interface block now mirrors `internal/credentials`; Phase 1 owns exporting them and updating login.md |
| Review 1, M3: no policy for an unreadable or other-version file | F3 to F6 and the invariant row on version refusal |
| Review 1, M4: `SLIVINGDOC_TOKEN` beside an association | D11 and F14 |
| Review 1, M5 and M6: `status` promised twice, `log` cannot report, no accessor exists | The two deliberate parameter rows and the `Runtime` accessor row |
| Review 1, M7: the canonical path rule was underspecified | The canonical-path parameter row names `resolvePath` and forbids `workspace.canonicalize` |
| Review 1, M8: the concurrency test contradicted its mechanism | The invariant row now names two processes |
| Review 1, M9 and M10: an unbounded entry count and an unowned size bound | The limits table, F24, and `Config.MaxFileSize` as the injectable field |
| Review 1, M11: recording in cases that poison the record | F14 to F17 keep the entry untouched; D13 bounds the write |
| Review 1, M12: `--prefix` broke the success criterion | D12 and F20 |
| Review 1, M13: same-named spaces of two accounts | The evidence bullet on `SpaceID` and D9 |
| Review 1, m1 to m4, n1 to n4: checklist regex, unsupported checklist claim, gate wording, `Storage` naming, `Locate` contract, write hook location, `serve` scenario | The checklist item, the journal entry below, the validation policy, `Target`, the configuration-directory parameter row, the `Runtime` accessor row, and the `serve` invariant row |

## Session journal

### 2026-10-03: design and README

Interviewed the maintainer to a complete design. The requirement was that
several sibling note directories hold notes from different hosted spaces, and
that entering one is enough to select its space. A subagent reviewed the design
critically and recommended keeping the file out of the notebook directory, which
the content rules confirmed. The maintainer settled the precedence order, the
recording points, the refusal on an unreachable remembered space, and the
requirement that the store leave room for further per-directory settings. The
error surface was then enumerated with the maintainer and became the failure
surface table below.

### 2026-10-03: critical review round 1

A second agent read the draft against the code and raised six blockers, of which
the load-bearing ones were structural. `OperationPath` returns an empty string
for the bare-command case, so the association key had to become the workspace
root, not the argument. The association as drafted never selected hosted mode,
so on a logged-out machine the feature would have pulled an S3 notebook into the
directory, and `s3Signals` would have refused it outright on any machine with an
AWS file present. `guardFirstPull` returns early for an already-pulled workspace
and admits any local file against an empty remote, so "the directory cannot
switch spaces" was false and re-pointing needed its own rule. The record's
endpoint contradicted itself between the parameters table and the invariant row,
and the README referenced an error table it did not contain.

This revision closes each of them: the key rule is a parameter row, D9 drops the
endpoint, D10 makes a remembered space host-only, the failure surface table F1
to F24 replaced the prose counts, F20 records the prefix consequence the draft
missed, and new invariant rows cover `serve`, S3 fallback, entry replacement
and version refusal. The store interface now mirrors `internal/credentials` (a
`File` value carrying the directory, with `Load`, `Save` and `CheckDir`)
because the drafted one could not reach the file from `Store`. `Config` now
injects `MaxFileSize` and `Load`, which makes the size limit a testable value
rather than "someone else's constant".

### 2026-10-03: phases written

The five phase files were written from this README. The maintainer had not
answered the two open questions, so the phases take the safer reading of each
and both choices are recorded here for review. F20 became a remembered prefix
rather than a bare refusal, so `Target` carries the prefix beside the space and
a resolved prefix that differs is the refusal. That keeps the README's promise
that a bare command reaches the same notebook, and it removes the silent
addressing of a different notebook that the prefix-less draft allowed. D9 is
unchanged: no endpoint is remembered, which holds while one account serves all
of a person's spaces and is why F10 and F11 keep today's meaning.

Two places needed the README's shared facts made concrete. The association key
needed the workspace root for a bare command, which requires root resolution to
move ahead of `resolveStorage` in `loadConfig`; Phase 2 owns that move and names
it in its specification. The write hook cannot be `resolveStorage`, because the
space a stored login really uses is only known after `buildService` minted a
token; Phase 3 puts the recorder in `Runtime.Pull` and `Runtime.Commit` behind
a condition table.

The checklist found two defects in the phase drafts as written: the file-layout
block in Phase 1 lost its leading markers, and an earlier draft put a concrete
token where a section had to name the space-source wording. Both are fixed.
Every test name is now declared in exactly one phase, and Phase 5 checks that
against the tree.
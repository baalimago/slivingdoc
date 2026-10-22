# Phase 4: hosted `space` backend and `space new`

**Status:** Complete (2026-10-19, session 3)

README: [README.md](./README.md)

## Goal

Give `space` one backend-aware surface for hosted storage: list every stored
login's spaces with their directories, create a space with `space new`, and set
the default with `space <name>`.

## Specification

### Command surface

`SpaceFlags` keeps `endpoint` and gains `storage`, `region`, `profile`,
`pathStyle`, and `prefix`, bound like the shared flags and their environment
variables. `--endpoint` keeps its hosted spelling `SLIVINGDOC_ENDPOINT`; an S3
endpoint comes from `--endpoint`, then `AWS_ENDPOINT_URL_S3`, then
`AWS_ENDPOINT_URL`, then the AWS configuration, which `s3store.Resolve`
reads. `cmd/login.SpaceCommand` accepts at most two positional arguments:

| Arguments | Mode |
| --- | --- |
| none | list, or open the picker in Phase 7 |
| `<name>` | set the default |
| `new <name>` | create |
| `new` | set the default to the target named `new` when a listing contains one; otherwise refusal naming the target to create |
| anything else | the existing “unexpected argument” refusal |

The Go flag parser stops at the first positional argument, so flags precede the
positional words, as they already do for `space <name>`. `space new new`
creates a target named `new`, so the positional word never makes a valid name
unreachable.

### Usage text

`spaceHelp`, the `SpaceCommand` description string, and the command list in
`internal/cli`'s `Usage` describe the three modes and every new flag. Both
strings leave `cmd/login` through the exported `SpaceCommand`: the assertion
reads `login.SpaceCommand(...).Help()` and `.Describe()`, because `spaceHelp`
is unexported. A new assertion names `new` and each flag, so an operator who
runs `slivingdoc space -h` learns the create form. The styled `space -h` prefix
assertion in `scenario_colour_linux_test.go` is rewritten to the new
description.

### Backend selection

`PrepareSpace` resolves one mode:

| Input | Result |
| --- | --- |
| `--storage hosted` | hosted |
| the backend resolves to hosted, with `--profile`, `--region`, `--path-style`, or `--prefix` | refusal naming that flag and that it names an S3 store |
| `--storage s3` | this phase refuses `space: S3 targets are not available in this build` |
| `--storage auto`, with an explicit S3 signal | the same phase-local refusal |
| `--storage auto`, otherwise | hosted |

An explicit S3 signal is `--profile`, `--region`, `--path-style`, or an
endpoint that matches no stored login. The environment spellings count as the
same signals: `AWS_PROFILE`, `AWS_REGION`, `AWS_DEFAULT_REGION`,
`AWS_ENDPOINT_URL`, `AWS_ENDPOINT_URL_S3`, `SLIVINGDOC_PATH_STYLE`, the
container credential variables, and the shared AWS files. `app.s3Signals` reads
the two set flags from the shared `*Flags`, so this phase gives it a narrow
view — an unexported interface with `regionSet() bool` and `pathStyleSet()
bool`, implemented by `*Flags` and by `*SpaceFlags` — instead of a second list
of variable names. `internal/app/storage.go` holds the interface and
`s3Signals`; `internal/app/space.go` implements it for `SpaceFlags`.
`--endpoint` is backend-relative: an endpoint that matches a stored login
selects hosted; another endpoint is an S3 signal. `SLIVINGDOC_TOKEN` is never
read as a credential and never refuses a command merely because it is set.

The hosted backend is chosen from the stored logins:

- `--endpoint` (normalized) selects the login stored for it;
- several logins with no endpoint are a refusal naming `--endpoint` when the
  command must select one (a create); a listing shows one group per login, and
  a set works from the listing and refuses only when more than one identity
  lists the name;
- no login is a refusal naming `slivingdoc login` when the command must select
  one (a create), or when `--storage hosted` names the backend, or when no other
  backend is reachable (Phase 6 owns that last case); a listing or a set with
  another reachable backend has no hosted group. This refusal is `space`'s own:
  it says a stored login is needed and never mentions `SLIVINGDOC_TOKEN`, which
  `space` ignores. `noLoginRefusal` stays as it is, because `serve`, `pull` and
  `commit` reach the token through it (`architecture/login.md`, `config.md`);
- an expired login that a create selects is refused before the site is
  contacted; in a listing under `auto`, and in the listing a set reads, it is
  degraded with a hint (Hosted listing), while `--storage hosted` with no other
  usable login refuses the command.

### `spaceBackend`

The README's interface is implemented once for hosted:

```go
type spaceBackend interface {
    Kind() settings.Kind
    Header() SpaceHeader
    List(ctx context.Context) ([]SpaceRow, error)
    Create(ctx context.Context, name string) (SpaceRow, error)
    Default() (settings.Target, bool)
    SetDefault(ctx context.Context, name string) error
}
```

A failed `List` returns the configured default and the matching recorded rows
with `Reachable: false`, plus a `*spaceListError` whose text is the sanitized
hint. The caller prints the rows, writes the hint to stderr, continues with the
other backends, and exits zero.

`Header().Endpoint` is the login's storage endpoint for hosted and the
effective endpoint for S3; it is the value `--endpoint` matches, so a header can
be passed back unchanged. `Header().Account` is the login's account;
`Header().KeyID` is S3 only. The site that issued a login appears in
`slivingdoc login`'s report, never in this header. Phase 6 prints `(AWS)` for the
provider's default endpoint and `(ambient)` for no profile, which are display
tokens rather than flag values.

### Hosted listing

Every stored login is listed in credentials-file order. Each group prints one
header:

```text
HOSTED  <storage endpoint>  <account>
```

The header names the login's storage endpoint (`Login.Endpoint`), which is the
value `--endpoint` and `SLIVINGDOC_ENDPOINT` match; directory matching uses the
same endpoint.

Rows are sorted by name. A row is `[markers]<name>[(<directories>)]  <access>  <owner>`:

- the marker is `*` for that login's default, `!` for an unreachable row, or
  both, in that order;
- directories come from settings entries whose target is hosted, whose name
  matches, and whose endpoint is empty or equal to the login's endpoint;
- directories appear in settings-file order, at most three, then `+N`;
- a row with no directory prints its name alone;
- access prints `write` or `read`, never `read and write`;
- owner prints the owner's email, or `a team` when empty;
- a row with `Reachable` false prints its name and directories only: the
  listing did not name its access or owner, and the `!` and its hint say why.

A login that cannot list keeps its default and matching recorded rows, marked
`!`, and its hint goes to stderr without stopping another login. An expired
login is handled the same way.

A login that lists successfully but no longer returns its stored default keeps
that default as a synthesized row marked `*!`, with a hint naming it, so a
default never disappears from the listing.

A hosted settings entry that no stored login reaches is shown under one
synthetic group:

```text
HOSTED  (no stored login)  —
```

with the row marked `!` and the hint:

```text
! this directory remembers hosted space "x", which no stored login reaches; run 'slivingdoc login'
```

The `! ` is part of the printed line in plain and styled output, not a `tui`
mark: the plain `Mark` of `internal/tui` prints nothing, and a hint must read
the same in both forms. The hint is `space`'s own and not `noCredentialRefusal`,
whose text names `SLIVINGDOC_TOKEN` and belongs to `serve`, `pull` and `commit`.

A group with no rows prints:

```text
No spaces yet; run 'slivingdoc space new <name>' to create one.
```

A group whose listing failed and that has nothing recorded prints its header
and its hint, and no such line: an empty store is not what happened.

A group with rows but no default keeps the existing hint on stderr:

```text
No default space; run 'slivingdoc space <name>' to choose one.
```

Fields are separated by two spaces, exactly as in the README examples.
Groups are separated by one empty line; no leading or trailing empty line.

### Hosted create

`space new <name>` requires exactly one login: `--endpoint` chooses it, several
without an endpoint refuse naming `--endpoint`, none refuses naming
`slivingdoc login`. A read-only login is refused before the site is contacted:

```text
error: space new: this login is read-only and cannot create a space; run 'slivingdoc login' and approve write access
```

Otherwise the command calls `sitelogin.CreateSpace`, prints one line, and
changes no default:

```text
Created hosted space research (write, ada@example.com) at https://api.slivingdoc.dev.
It is not the default; run 'slivingdoc space research' to make it the default.
```

The follow-up hint repeats the flags that selected the login, `--endpoint` and
`--storage hosted` when each was set, so the printed command selects the same
identity again: `run 'slivingdoc space --endpoint <endpoint> research' ...`.
With one login and no flag the hint carries no flag, as above.

The site's refusals map to the README's one-line messages: `name_taken`,
`space_limit`, `invalid_name`, `access_denied`, `busy`, and `pool_empty`. A
name outside the grammar is refused locally by `storage.ValidateNewSpaceName`
before any request.

### Setting the default

`space <name>` lists every backend, looks for the name, and refuses when it is
absent or listed by more than one identity. If no backend lists the name and at
least one listing failed, the refusal says the listing was incomplete rather
than claiming the name is absent. A hosted identity is chosen with
`--endpoint`; the refusal names that flag and lists the storage endpoints of the
group headers, never the sites, so every listed value can be passed back
unchanged. The chosen backend stores the
default under `credentials.lock` and re-checks that the login it listed with is
still stored:

```text
The default space is now research (write, ada@example.com).
```

### Output stream rules

stdout carries the listing, the create line, and the default line. stderr
carries hints, the picker's prompt in Phase 7, and the one-line refusal. A
successful list, create, and set exit zero. A degraded list exits zero. A
refusal exits nonzero. A hint that reports a degraded or unreachable identity
begins with a literal `! `; the no-default hint keeps its existing text and no
mark.

### Shared rendering and picker helpers

`slivingdoc space` shares helpers with the login flow. The space surface must
not change the login flow's listing output, in plain mode or styled mode.

| Symbol | File | Fate |
| --- | --- | --- |
| `spacesTable` | `internal/app/present.go` | Kept unchanged. It remains the renderer for space listings that the login flow prints. |
| `pickSpace` | `internal/app/present.go` | Kept unchanged. It remains the picker helper that the login flow uses. The space picker in Phase 7 is a new picker and does not call `pickSpace`. |
| `describeSpace` | `internal/app/login.go` | Kept unchanged. It remains the login flow's per-space formatting helper. |
| `Space.list` | `internal/app/space.go` | Replaced. The space surface uses a new renderer for the Phase 4 listing so it can print hosted groups, headers, and directory annotations without changing login output. |
| `login`'s listing and picker | `internal/app/login.go` | Left to the login flow. The login command continues to call `spacesTable`, `pickSpace`, and `describeSpace` as it does today, and its plain and styled output stays byte-identical. Rewrite only the `space` output assertions in `internal/app/login_test.go`. |

Before Phase 7, the terminal path of `Space.Run` keeps calling `pickSpace`, so
it still draws the login picker table; Phase 7 replaces that call with the new
space picker. Phase 4's scenarios run on a pipe, so they assert the plain form
only.

### Invariants and limits

| Rule | Mechanism | Test |
| --- | --- | --- |
| A token is never a `space` credential | env token is not read | `TestSpaceIgnoresToken` |
| A failed login never hides another | per-login list; errors collected as hints | `TestSpaceListsEveryLogin` |
| A recorded or default target stays visible | synthesized rows with `Reachable=false` | `TestSpaceDegradedListShowsDefaultAndRecords` |
| An orphan record stays visible | synthetic no-login group | `TestSpaceShowsOrphanRecord` |
| A stored default absent from a listing stays visible | synthesized `*!` row and hint | `TestSpaceShowsMissingDefault` |
| Create never changes the default | create path does not call `SetDefault` | `TestSpaceNewLeavesTheDefault` |
| Create is explicit | only `sitelogin.CreateSpace` creates | `TestSpaceNewCallsCreateOnce` |
| A record cannot become the command target | the backend takes its target from flags and the environment; a record only supplies the directory column | `TestSpaceNeverTargetsARecord` |
| The access word is `write`/`read` | `spaceAccess` helper | `TestSpacePlainRows` |
| At most three directories, then `+N` | formatter | `TestSpaceRowsShowThreeDirectories` |
| Login listing output is unchanged | the space surface does not edit `spacesTable`, `pickSpace`, or `describeSpace` | `TestLoginOutputUnchanged` |

### Documentation

| Doc | What must become true |
| --- | --- |
| `architecture/login.md` | Update the hosted plain listing: the storage endpoint in the group header, directory annotations, access word, owner, the literal `! ` hint prefix, and `*`/`!` marker rules. Update the no-login group and orphan, no-spaces, and no-default hint lines, and the `--storage hosted` no-login refusal, which is `space`'s own and names no token. Update the default line and the hosted refusal lines for read-only access, `name_taken`, `space_limit`, `invalid_name`, `access_denied`, `busy`, and `pool_empty`; the multi-identity refusal lists storage endpoints. Name `space new <name>` and its refusal before `sitelogin.CreateSpace`, and the rule for a target named `new`. Correct the `cmd/login/login.go` file-map claim that `space` takes at most one positional argument, and the `SpaceFlags` row that lists only `--endpoint`. |
| `architecture/cli.md` | Change the `cmd/login/login.go` file-map claim from at most one positional argument to the `space`, `space <name>`, and `space new <name>` forms. Update the `Space.Run` flow to match. State that flags precede the positional words, and list `--storage`, `--endpoint`, `--region`, `--path-style`, and `--prefix`. |
| `architecture/running.md` | Replace the `space` transcript under “The default space” — the `* notes (read and write), owned by …` lines and `The default space is now team (read only), owned by …` — with the group header (storage endpoint and account), the directory annotations, the `write`/`read` rows, and `The default space is now <name> (<access>, <owner>).` The `login` transcript above it stays byte-identical. Update the picker sentence to `space`'s new picker. |

### Files

| File | Change |
| --- | --- |
| `cmd/login/login.go` | Extend `SpaceCommand` to support `space`, `space <name>`, and `space new <name>`, and bind the new `SpaceFlags`. |
| `internal/app/space.go` | Implement the hosted `spaceBackend`, including listing groups, create through `sitelogin.CreateSpace`, and default selection rules. |
| `internal/app/present.go` | Add the plain hosted `space` listing renderer that prints headers, rows, and directory annotations without changing `spacesTable`. |
| `internal/app/storage.go` | Replace the `*Flags` parameter of `s3Signals` with the narrow flag-set view, so `*SpaceFlags` reports the same S3 signals without a second list of variable names. |
| `internal/app/login.go` | Keep `describeSpace` and login listing behavior unchanged while `space` uses its own renderer. |
| `internal/cli/cli.go` | Extend `Usage`'s command description so the `space` line names the three modes. |
| `internal/app/login_test.go` | Keep login listing output byte-identical in plain and styled modes. Rewrite the `space` output assertions in `TestSpaceListsAndSetsTheDefault`, `TestSpaceChoosesTheLogin`, and `TestSpaceRefusesAnExpiredLogin` to the Phase 4 plain form. |
| `internal/integrationtest/scenario_login_test.go` | In `TestScenarioAccountLoginLifecycle` and `TestScenarioTwoLoginsNeedAnEndpoint`, rewrite the exact `space` output assertions to the Phase 4 plain form. In `TestScenarioLoginThenPullAndCommit`, keep the login stderr listing assertion unchanged. |
| `internal/app/space_test.go` (new) | Add the hosted unit tests, including `TestSpaceExpiredLoginIsDegraded` and `TestSpaceExpiredLoginBesideAUsableOne` for degraded expired-login listings, and the scenario helpers the phase names. |
| `internal/integrationtest/scenario_space_hosted_test.go` (new) | Add the hosted `space` scenario test for plain output, degraded listing, and exit codes. |
| `internal/cli/cli_test.go` | Add `TestSpaceUsageNamesTheModes`, which asserts `login.SpaceCommand(...).Help()` and `.Describe()`, and `Usage`, name `new` and each new flag; `spaceHelp` is unexported, so the test reads it through the command. |
| `internal/integrationtest/scenario_colour_linux_test.go` | Rewrite the styled `space -h` prefix assertion to the new command description. |

## Integration contract

| Trigger | Collaborators or fakes | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| `space` with one login and two spaces | `sitetest`, planted settings | header, rows, default `*` | one spaces request | a token read, a settings write |
| `space` with two logins | two `sitetest` sites | two groups in file order | one request per login | one login hiding another |
| Existing `space` output assertions in `TestScenarioAccountLoginLifecycle` and `TestScenarioTwoLoginsNeedAnEndpoint` | `sitetest`, planted settings | Phase 4 plain form: group header; rows with directory annotations, `write` or `read`, hosted owner, and `*`, `!`, or `*!` markers where applicable; `The default space is now <name> (<access>, <owner>).` | the existing requests and settings writes | a change to login output, including `TestScenarioLoginThenPullAndCommit` |
| `space` with an expired login | planted credentials file | its group marked `!`, hint, others listed | none | a site request for the expired login |
| `space` with an orphan hosted record | planted settings | the synthetic no-login group | none | a site request |
| `space` with a planted settings record that names another endpoint or name | planted settings, environment, flags | the listing uses the login chosen by flags and environment, and the create/list target never changes | one spaces request | a record becoming the list or create target |
| `space` while `SLIVINGDOC_TOKEN` is set | environment | the same hosted listing | none | reading the token |
| `space new research` | `sitetest` | create line, no default line | one create request | a default change |
| `space new` while a target named `new` is listed | `sitetest`, planted settings | the default line, no create line | one spaces request | a create |
| `space new` with a read-only login | planted credentials file | the read-only refusal | none | any site request |
| `space new` with two logins and no endpoint | two `sitetest` sites | refusal naming `--endpoint` | none | a create |
| `space research` | `sitetest` | the default line | one `SetDefaultSpace` | a create |
| `space research` listed by two logins | two `sitetest` sites | refusal naming `--endpoint` | none | a default change |
| `space --storage hosted --region eu-north-1` | none | refusal naming `--region` | none | any site or AWS call |
| `space --storage s3` with a reachable identity | fake admin | the explicit S3 listing | one `ListBuckets` | a probe, a create |

## Acceptance criteria

| Criterion | Test |
| --- | --- |
| A hosted listing prints one header per login and sorted rows | `TestSpaceListsEveryLogin` |
| The default is marked `*` | `TestSpacePlainRows` |
| Directories match kind, name, and endpoint; order is settings-file order | `TestSpaceDirectories` |
| A planted record cannot become the command's own list or create target | `TestSpaceNeverTargetsARecord` |
| More than three directories print three and `+N` | `TestSpaceRowsShowThreeDirectories` |
| A row with no directory prints no parentheses | `TestSpacePlainRows` |
| Access prints `write` or `read` | `TestSpacePlainRows` |
| A failed login keeps its default and records visible | `TestSpaceDegradedListShowsDefaultAndRecords` |
| A synthesized hosted row prints no access or owner, and a failed group with nothing recorded prints no `No spaces ...` line | `TestSpaceDegradedListShowsDefaultAndRecords` |
| An orphan hosted record is visible with its hint | `TestSpaceShowsOrphanRecord` |
| A stored default absent from the listing is visible and marked `*!` | `TestSpaceShowsMissingDefault` |
| `space` ignores `SLIVINGDOC_TOKEN` | `TestSpaceIgnoresToken` |
| `space new` creates and prints the exact lines | `TestSpaceNewHosted` |
| `space new` leaves the default unchanged | `TestSpaceNewLeavesTheDefault` |
| A target named `new` is selected by `space new` | `TestSpaceNewNamedNew` |
| The create hint repeats `--endpoint` when it was given | `TestSpaceNewHintFlags` |
| A read-only login is refused before the site | `TestSpaceNewReadOnly` |
| Several logins without `--endpoint` are refused for a create | `TestSpaceNewNeedsOneLogin` |
| An invalid name is refused locally | `TestSpaceNewInvalidName` |
| `space <name>` stores the default | `TestSpaceSetDefault` |
| A name listed by two identities is refused, and the refusal lists storage endpoints that `--endpoint` accepts | `TestSpaceSetDefaultAmbiguous` |
| `--storage hosted` with an S3 flag is refused | `TestSpaceHostedRefusesS3Flags` |
| An S3 signal in auto mode selects an S3 listing | `TestSpaceAutoS3SignalLists` (Phase 6) |
| `--storage s3` lists the explicit S3 backend | `TestSpaceS3ExplicitBackend` (Phase 6) |
| `space -h` names the three modes and every new flag | `TestSpaceUsageNamesTheModes` |
| A degraded list and a create exit zero | `TestScenarioSpaceHosted` |
| The degraded hint line is byte-exact, with its literal `! ` | `TestScenarioSpaceHosted` |
| The plain hosted output is exactly the hosted examples under Output | `TestScenarioSpaceHosted` |
| The old `space` transcript is gone and the `login` transcript is untouched | `rg -n 'owned by' architecture/running.md` hits the `login` block only |
| Login's listing output is unchanged in plain and styled modes | `TestLoginOutputUnchanged` |
| Existing `space` output assertions expect Phase 4 plain output: group header, directory annotations, `write` or `read`, hosted owner, `*`, `!`, or `*!` markers where applicable, and `The default space is now <name> (<access>, <owner>).` An expired login in a listing is degraded. | `TestSpaceListsAndSetsTheDefault`, `TestSpaceChoosesTheLogin`, `TestSpaceRefusesAnExpiredLogin`, `TestSpaceExpiredLoginIsDegraded`, `TestScenarioAccountLoginLifecycle`, `TestScenarioTwoLoginsNeedAnEndpoint` |

## Error coverage

| Failure | Expected outcome | Test |
| --- | --- | --- |
| no stored login with `--storage hosted` | refusal naming `slivingdoc login` | `TestSpaceWithoutLogin` |
| several logins without `--endpoint` for a create | refusal naming `--endpoint` | `TestSpaceNewNeedsOneLogin` |
| an expired login in a listing under `auto`, or beside another usable login | its group is degraded with a hint, exit 0 | `TestSpaceExpiredLoginIsDegraded`, `TestSpaceExpiredLoginBesideAUsableOne` |
| the only login, or the `--endpoint` login, is expired and `--storage hosted` asked for the backend | refusal naming `slivingdoc login` again, exit nonzero | `TestSpaceHostedExpiredLoginRefuses` |
| an expired login that a create would use | refusal naming `slivingdoc login` again | `TestSpaceExpiredLogin` |
| site answers 401 | refusal naming `slivingdoc login` again | `TestSpaceListRevoked` |
| one login cannot list | hint on stderr, exit 0 | `TestSpaceDegradedListShowsDefaultAndRecords` |
| orphan hosted record | hint, row with `Reachable=false` | `TestSpaceShowsOrphanRecord` |
| stored default absent from the listing | `*!` row with its hint, exit 0 | `TestSpaceShowsMissingDefault` |
| unreadable settings file | `unusableSettings` refusal | `TestSpaceUnusableSettings` |
| the backend resolves to hosted with `--profile`, `--region`, `--path-style`, or `--prefix` | refusal naming that flag | `TestSpaceHostedRefusesS3Flags` |
| `--storage s3` identity fails | explicit refusal | `TestSpaceS3ExplicitFailureRefuses` (Phase 6; replaces the phase-local refusal) |
| auto-mode S3 identity fails | degraded listing and hint, exit 0 | `TestSpaceAutoS3IdentityFailureDegrades` (Phase 6; replaces the phase-local refusal) |
| `space new` with no target named `new` listed | refusal naming the target to create | `TestSpaceNewWithoutName` |
| invalid name | `storage.ValidateNewSpaceName` message | `TestSpaceNewInvalidName` |
| read-only login | the README read-only refusal | `TestSpaceNewReadOnly` |
| site answers 409 `name_taken` | the README duplicate refusal | `TestSpaceNewNameTaken` |
| site answers 403 `space_limit` | the README limit refusal | `TestSpaceNewLimit` |
| site answers 403 `access_denied` | the README read-only refusal | `TestSpaceNewAccessDenied` |
| site answers 429 or 503 | retryable refusal naming the cause | `TestSpaceNewBusy` |
| `space <name>` absent from the listing | refusal naming what is listed | `TestSpaceSetUnknown` |
| `space <name>` listed twice | refusal naming `--endpoint` | `TestSpaceSetDefaultAmbiguous` |
| login changed between listing and store | “run the command again” | `TestSpaceSetLoginChanged` |

## Implementation notes

Worker: phase-4 agent, 2026-10-19, session 3 (continued from the partial tree the
previous session left). The phase is complete: code, unit tests, integration
scenarios, and the architecture docs this phase touches are in place.

### What landed

- `internal/app/space.go` holds the README's `spaceBackend` interface:
  `SpaceFlags` binds `--storage`, `--endpoint`, `--region`, `--profile`,
  `--path-style`, and `--prefix`; `SpaceHeader`/`SpaceRow` are the listing
  values; `parseSpaceArgs` handles `space`, `space <name>`, `space new`, and
  `space new <name>`. `PrepareSpace` parses the positional words first, refuses
  `--storage s3` before any file is read, then resolves the hosted logins.
  `hostedSpace` lists every stored login, sorts rows, annotates directories
  (three then `+N`), synthesizes a missing default and orphan records, creates
  through `sitelogin.CreateSpace`, and stores the default under the credentials
  lock with the existing re-check (`storeDefault`, shared with the login flow).
- `internal/app/present.go` owns the space listing renderer (`Space.render`,
  `Space.hints`, `spaceRowLine`, `spaceAccess`, `spaceOwner`, `headerAccount`),
  beside the unchanged login renderers `spacesTable`, `pickSpace`, and
  `describeSpace`. The plain form is the contract; the styled form carries the
  same fields with colour.
- A failed listing and a successful listing with missing targets are different
  signals: `spaceListError.failed` marks only a failed listing, so `space
  <name>` says the listing was incomplete only when it really was, and an
  unlisted default is refused as absent.
- S3 remains phase-local: `--storage s3` refuses before any configuration file,
  auto with an S3 signal refuses naming the signal and both fixes, and
  `--profile`, `--region`, `--path-style`, and `--prefix` refuse beside a hosted
  backend. `s3Signals` takes the narrow `s3FlagSignals` interface (`regionSet`,
  `pathStyleSet`) implemented by `*Flags` and `*SpaceFlags`.
- `cmd/login/login.go` accepts two positional words and has the new `spaceHelp`
  text; `internal/cli/cli.go`'s `Usage` names `space new`.
- Tests: `internal/app/space_test.go` covers every acceptance and error row;
  `internal/integrationtest/scenario_space_hosted_test.go` proves the plain
  output, the degraded exit-zero listing, and the literal `! ` hint. The existing
  `space` assertions in `internal/app/login_test.go` and
  `internal/integrationtest/scenario_login_test.go` were rewritten to the
  Phase 4 plain form, `TestSpaceUsageNamesTheModes` was added, and the styled
  `space -h` prefix assertion was updated.
- Docs: `architecture/login.md` describes the hosted groups, markers,
  directories, hints, create and default selection; `architecture/cli.md`
  describes the three positional forms and the flags; `architecture/running.md`
  carries the new transcript and command table row; `architecture/config.md`
  and `architecture/decisions.md` record that `space` reads the settings only
  for its directory column and never to choose its target.

### Decisions

Recorded by the phase-4 agent, 2026-10-19, session 3.

1. A listing without `--endpoint` shows one group per stored login; `--endpoint`
   narrows it to that login. A create without `--endpoint` still refuses when
   several logins are stored. This follows the phase text "a listing shows one
   group per login" and keeps `space <name>` usable without a flag when only
   one identity lists the name.
2. Only explicitly set hosted-mode S3 flags refuse `--storage hosted`; S3
   environment variables and shared files are auto-mode signals. This avoids
   refusing an explicitly hosted command merely because a developer has
   `~/.aws/config`.
3. The hosted default row is synthesized by `hostedSpace.List` with
   `Reachable: false`; `setDefaultFrom` ignores unreachable rows, so a deleted
   space can never become the default.
4. A successful listing that merely omits a recorded or default target is not a
   failed listing. `spaceListError.failed` separates the two, so a selection
   never claims the listing was incomplete when the site answered it fully.
5. `--storage s3` is refused immediately after the positional words, before the
   credentials or settings file is touched; an auto-mode S3 signal is refused
   after the environment is read, because it needs the S3 signals.

### Commands

| Command | Result |
| --- | --- |
| `go build ./...`, `go vet ./...`, `gofumpt -l .`, `staticcheck ./...`, `go fix -diff ./...` | clean |
| `go test ./internal/app/ ./internal/cli/ ./internal/integrationtest/ -count=1` | pass |
| `make qa` | pass (gofumpt, vet, staticcheck, `go fix`, the race suite at `-count=3` with the coverage floor, npm) |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | 3 clone groups: the `LoginFlags`/`SpaceFlags` `Bind` pair is declarative flag registration with different constants, and the other two are pre-existing test tables; none actionable |
| `rg -n 'owned by' architecture/running.md` | one hit, in the login transcript only |

## Review findings

Self-review by the phase-4 agent, 2026-10-19, session 3.

1. The earlier tree marked every hint-carrying listing as failed. Fixed by the
   `failed` field and `TestSpaceSetUnlistedDefault`.
2. `--storage s3` still read the credentials file before refusing. Moved the
   refusal before `credentialsFile`; the then-current refusal test planted an
   unreadable settings file to prove neither file was touched.
3. `architecture/login.md` claimed an explicit `--storage hosted` login that
   cannot list refuses the command. The code degrades a revoked or unreachable
   login and refuses only the expired one before contact; the doc now says so.
4. The space renderer lived in `space.go` while `present.go` owns presentation.
   Moved it without changing the login renderers or their output.
5. The `space` command reads the settings file for its directory column, which
   three `architecture/config.md` sentences and decision 49 denied. All four
   now name `space` and limit its use to display.

### Review 2 (2026-10-20, session 11)

Round 2 re-ran `make qa` (exit 0, coverage 89.4 %, 35 npm tests) and rechecked
the phase's hosted surface. The listing, create, default, degrade, orphan and
refusal paths hold (`TestScenarioSpaceHosted`, `TestSpaceRowsShowThreeDirectories`,
`TestSpaceNeverTargetsARecord`, `TestSpaceHostedRefusesS3Flags` pass). Three
findings, all documentation. None is a blocker or major, so the phase keeps
its completion status, but the fixes belong to it:

- **R2-04 (minor).** `spaceHelp` (`cmd/login/login.go`) says an S3 group needs
  an S3 flag, an AWS variable, or a recorded target, and that a machine with
  only a hosted login reads no AWS configuration. `Space.s3Selected` also
  treats an existing `~/.aws/credentials` or `~/.aws/config` as a signal,
  which `architecture/login.md` states correctly. Correct the help sentence and
  the acceptance assertion that pins it.
- **R2-06 (minor).** The README refusal example says
  `error: space new: x must be 3 to 63 ...`; the shipped text is
  `space new: storage: invalid new space name: "x" must be 3 to 63 ...`.
  Align the example with the error wrapper, or unwrap the grammar error.
- **R2-07 (minor).** The README and `architecture/config.md` say an unreadable
  settings file refuses `space`. `PrepareSpace` skips the read in create mode,
  so `space new` succeeds. Scope the sentence to the listing and set paths.

No hosted behavior changed in this review.

### 2026-10-20: Review-round-2 remediation (session 14)

Closed the three minor documentation findings without changing hosted
behavior. R2-04 now names the shared AWS files as S3 listing signals and keeps
the rule that those files alone do not select S3 for `space new`; the CLI test
pins both. R2-06 keeps the storage grammar error wrapped and aligns the README
example and `architecture/login.md` with the shipped line. R2-07 limits the
unreadable-settings refusal to listing and set operations and states that
create mode does not read that file. The first focused run found a
line-wrap-sensitive assertion; whitespace normalization fixed it.

| Command | Result |
| --- | --- |
| `go test ./internal/cli ./internal/app -count=1` | pass |
| `make qa` | exit 0; 89.3% coverage against the 70% floor; all 35 npm tests passed, none skipped |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | three acceptable groups: command-specific flag registrations, integration/notebook fixture tables, and distinct Git merge test cases; no actionable duplication |

### Review 3 (2026-10-20, session 16)

The round-3 review confirmed the hosted listing, create, default, and refusal
paths against the current code and the named tests. The two findings below are
minor evidence gaps, not observed behavior failures. Phase 4 remains Complete
under the review reopen rule. At review time, the worklog was not ready for
final closure until both tests passed; session 17 closed those gaps.

- **R3-01 (minor).** The scoped-settings documentation says `space new` does
  not read or write `workspaces.json` (`architecture/config.md:75`).
  `PrepareSpace` enforces this with `if mode != spaceCreateMode` before
  `spaceSettings` (`internal/app/space.go:239-244`), but
  `TestSpaceUnusableSettings` only covers a listing refusal
  (`internal/app/space_test.go:634-645`). The create
  tests do not plant malformed settings or prove the file stays unchanged.
  Add a create test that uses malformed `workspaces.json`, completes creation,
  and verifies the file bytes are unchanged.
- **R3-02 (minor).** The README refusal example for `space new x` gives the
  complete plain stderr line (README.md:411-412), but no test pins that public
  line. `TestSpaceNewInvalidName` checks `ErrInvalidNewSpaceName`, a substring,
  and that no POST occurs; it does not assert the wrapped message
  (`internal/app/space_test.go:396-407).
  `TestSpacePickerRefusesAnInvalidS3Name` checks the app error, not the CLI's
  `error: ...` rendering (`internal/app/space_test.go:936-948`,
  `internal/cli/route.go:125-135`). Add a black-box
  CLI scenario that asserts the exit code, empty stdout, and exact stderr.

The review also confirmed that the R2-04 help sentence and its CLI assertion,
the R2-06 refusal wording, and the R2-07 settings-read sentence match the
current tree. `TestSpaceUsageNamesTheModes`, `TestSpaceNewInvalidName`, and
`TestSpaceUnusableSettings` passed; the last test proves the listing refusal,
not the create-mode exception described in R3-01.

### 2026-10-20: Review-round-3 remediation (session 17)

Closed R3-01 with `TestSpaceNewDoesNotReadOrWriteSettings`. It creates a hosted
space through the login rig with malformed `workspaces.json`, checks the
created output, and proves the file bytes did not change. Closed R3-02 with
`TestScenarioSpaceNewInvalidNameRefusal`, which pins the nonzero exit, empty
stdout, and complete stderr at the CLI boundary. Phase 4 remains Complete;
these were minor evidence gaps.
The first quoted app test filter matched no test under the tool invocation; the
unquoted named filter below passed.

| Command | Result |
| --- | --- |
| `go test ./internal/app -run TestSpaceNewDoesNotReadOrWriteSettings -count=1` | pass |
| `go test ./internal/integrationtest -run TestScenarioSpaceNewInvalidNameRefusal -count=1` | pass; the grammar refusal required no site or S3 backend |
| `make qa` | exit 0; coverage 89.3% against the 70% floor; all 35 npm tests passed, none skipped |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | exit 0; three acceptable clone groups: separate login/space flag registrations, the integration/notebook conflict test data at distinct contract layers, and distinct Git merge table cases; no actionable duplication |

### V5-02 re-check (resolved, minor; evidence ownership)

`Files` and `Acceptance criteria` now cite `TestSpaceRefusesAnExpiredLogin` in
`internal/app/login_test.go` and `TestSpaceExpiredLoginIsDegraded` in
`internal/app/space_test.go` for the expired-login refusal and degraded-list
behavior.

### V6-04 (note, evidence ownership)

The Acceptance criteria and Error coverage rows name phase-local S3 refusals
that Phase 6 replaced. Point to the final Phase 6 behavior tests.

### Resolution (2026-10-20, session 20)

V5-02 and V6-04 are resolved. The expired-login rows name
`TestSpaceRefusesAnExpiredLogin` and `TestSpaceExpiredLoginIsDegraded`. The S3
rows now name `TestSpaceAutoS3SignalLists`, `TestSpaceS3ExplicitBackend`,
`TestSpaceAutoS3IdentityFailureDegrades`, and
`TestSpaceS3ExplicitFailureRefuses`, which prove the final Phase 6 behavior.
Phase 4 remains Complete.

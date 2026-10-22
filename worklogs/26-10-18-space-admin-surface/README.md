# slivingdoc space admin surface worklog

**Status:** README signed off. All eight phases are complete, and the first post-close review (session 9) is resolved by session 10 ([review-2026-10-20.md](./review-2026-10-20.md)). Sessions 12 through 15 completed the review-round-2 remediations for Phases 6, 7, 4 and 5. Every review-round-2 finding is resolved. Session 17 closed the two minor Phase 4 test-evidence gaps from review round 3, and session 18 closed R3-03 by correcting the Phase 6 specification. Every review-round-3 finding is resolved. Session 20 resolved V5-02 and all V6 findings without reopening a phase; the verdict is **Ready**. Session 21 recorded the shared-config profile form in the architecture and help text; no behavior changed, so the verdict remains Ready. The phases remain Complete under the reopen rule. The Feedback index tracks every finding.

**Architecture:**
[`../../architecture/login.md`](../../architecture/login.md),
[`../../architecture/config.md`](../../architecture/config.md),
[`../../architecture/cli.md`](../../architecture/cli.md),
[`../../architecture/s3store.md`](../../architecture/s3store.md),
[`../../architecture/running.md`](../../architecture/running.md),
[`../../architecture/hosted-mode.md`](../../architecture/hosted-mode.md),
[`../../architecture/tui.md`](../../architecture/tui.md),
[`../../architecture/security.md`](../../architecture/security.md),
[`../../architecture/errors.md`](../../architecture/errors.md),
[`../../architecture/testing.md`](../../architecture/testing.md)

**Cloud repository:** a separate checkout, `slivingdoc-cloud`. Every path
prefixed `cloud:` is relative to its root. The checkout is present and writable
at `/home/imago/Projects/private/slivingdoc-cloud`; export `CLOUD_REPO` to that
root for every `cloud:` command. Its `make qa` is the full gate and includes
`make cloud`, the storage gateway's typecheck and tests.

The environment is set up, so no phase waits for a person: the checkout, its
Node dependencies, the Playwright browsers under `~/.cache/ms-playwright`, the
Docker daemon that `internal/tests3` leases, and the pinned libgit2 are all in
place. Phase 2 edits the cloud checkout directly, and Phase 8's cloud gate runs
it in place.

**Provenance:** split at design sign-off from one earlier worklog, whose
directory was removed when the split was made. This worklog owns creating, listing, and
choosing a target on both backends. The sibling
[`26-10-18-directory-bindings`](../26-10-18-directory-bindings/README.md) owns
the one-directory MCP surface and how `serve`, `pull`, `commit`, `status`, and
`log` resolve and record a target; it starts after this worklog's Phases 1, 4,
5, and 6 ship, though its first phase is independent and may run earlier. This
worklog changes no MCP tool, no notebook identity, and no probe proof.

## Goal

Today a hosted space can be created only on the site, an S3 bucket only
outside slivingdoc, and the CLI can neither list nor create either. Give both
backends one product surface: a person lists, creates, and selects a default
target, and sees which directories remember each target, through one `space`
command.
`slivingdoc space new <name>` creates a hosted space through the site's CLI
route and an S3 bucket through the S3 API. An S3 target is named by its
effective endpoint and region, and by an AWS shared-config profile when one is
chosen; slivingdoc stores no S3 credential. The target type and the settings
file gain both backends, and the defaults list is keyed by the store identity.

What must stay true: `current` remains the only accepted-state authority; a
pack's bytes exist before any manifest references it; publication is a
conditional ETag replacement; no Git vocabulary, key, token, private path, or
secret reaches caller-facing text; CGo stays inside `internal/git2`; the AWS
SDK stays inside `internal/s3store`; no credential is stored in the settings
file; and `space` never takes its own target from a directory record.

## Glossary

| Term                   | Meaning                                                                                                                                              |
| ---------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------- |
| **Target**             | A named notebook on a backend: `{kind, endpoint, region, profile, name, prefix, pathStyle}`.                                                         |
| **Kind**               | `hosted` (the slivingdoc hosted storage API) or `s3` (an S3-compatible store).                                                                       |
| **name**               | The hosted space name or the S3 bucket name.                                                                                                         |
| **prefix**             | The notebook prefix inside a store; it says which notebook of the store the target names.                                                             |
| **path style**         | S3 addressing that puts the bucket in the URL path. A custom endpoint and a dotted name on the default AWS endpoint both require it.                  |
| **profile**            | An AWS shared-config profile name. A selector, never a secret. Empty means the ambient AWS credential chain.                                         |
| **Record**             | A target stored for one absolute directory path in `workspaces.json`, also called a directory binding. Keyed by path alone.                          |
| **Directory**          | The absolute host path that a record is keyed by; `space` shows it beside a target.                                                                  |
| **Default**            | A target stored once per store key, used when no flag, environment, or record names a target. A hosted default is the login's per-endpoint default space in `credentials.json`; the store-keyed list holds S3 targets only. |
| **Store key**          | `{kind, endpoint, region, profile}`: the identity of a store before its notebook name is known.                                                     |
| **Effective endpoint** | The normalized endpoint after flags, environment, profile, and AWS configuration resolve. Empty means the provider's default.                        |
| **Effective region**   | The region after flags, environment, profile, and AWS configuration resolve.                                                                         |
| **Backend**            | One implementation of the `space` command's view of a store: hosted or S3.                                                                           |
| **Portable name**      | The one creation grammar, valid on hosted storage, S3, and R2.                                                                                       |
| **CLI key**            | The account login key that `slivingdoc login` stores. It lists spaces, mints space tokens, and creates spaces.                                       |
| **Space token**        | A one-hour token minted from a CLI key for one space.                                                                                                |
| **Space row**          | One listed target, with the directories whose records name it; a recorded or default target the listing did not return is still shown, marked unreachable. |

## Flows

### `space`: list, create, and choose a default

```text
slivingdoc space [--storage s3|hosted|auto] [--profile P] [--endpoint E]
                 [--region R] [--path-style] [--prefix P]
  |
  v
resolve the backend (no notebook path; records are read only for the
  directory column, never to choose the command's own target)
  |
  +--> hosted: every stored login -> GET /cli/v1/spaces
  |            a failure of one login is reported and does not stop the rest
  |
  +--> s3:     the current chain, plus every profile recorded in a record or
               default -> ListBuckets, one group per identity
               a bucket is never probed; ListBuckets is the only bucket call
               (the header's key id comes from Admin.Identity)
               a recorded target that no reachable identity lists is still
               shown, marked unreachable, so a binding never disappears
  |
  v
rows = targets, enriched with the directories whose records name them
  |
  +--> not all three are terminals: print the rows, the default marked *
  |
  +--> stdin, stdout and stderr are all terminals:
         picker rows: target, name, directories, access, owner, default,
           then one create row per backend
           choose a row      -> SetDefault for the resolved store key
           choose create     -> prompt for a name -> backend.Create(name)
                                -> refresh the list -> select the new target
```

```text
slivingdoc space <name>        set the default; the name must be in the listing
slivingdoc space new <name>    create; print the target; do not change the default
```

`space` ignores `SLIVINGDOC_TOKEN`: a token reaches one space and cannot list,
create, or mint for an account. Every other command keeps its current token
behavior.

A record reaches this command in one direction only: its target supplies the
directories printed beside a row. No flag, no default, and no listing entry is
derived from a record.

### `space new`: one identity, one create

```text
slivingdoc space [--storage ...] [--profile P] [--endpoint E]
                 [--region R] [--path-style] [--prefix P] new <name>
  |
  v
resolve one create identity from the flags and the environment only
  |
  +--> hosted: the one stored login the flags select
  |            -> sitelogin.CreateSpace -> POST /cli/v1/spaces
  |
  +--> s3:     the current chain, or the profile and endpoint the flags name
  |            -> s3store.Admin.CreateBucket
  |
  v
print the created target; the default is unchanged
```

A recorded or default S3 identity is a listing and default aid, never a create
target. The picker's S3 create row uses the same current identity.

## API additions

### Cloud: `POST /cli/v1/spaces`

Creates one space for the account a CLI key belongs to. It is the only new
route; `GET /cli/v1/spaces` keeps its meaning and gains a method branch.

```text
POST /cli/v1/spaces
Authorization: Bearer <CLI key>
Content-Type: application/json

{"name":"team-notes"}

201 {"space":{"name":"team-notes","owner":"ada@example.com","access":"write"}}
```

| Outcome           | Status | Error                | Cause                                                                           |
| ----------------- | ------ | -------------------- | ------------------------------------------------------------------------------- |
| Created           | 201    | —                    | The space exists, the key's account owns it, and the key may write              |
| Bad body          | 400    | `invalid_request`    | Not one strict object with one string `name`; extra, duplicate, or null members |
| Bad name          | 400    | `invalid_name`       | Outside the portable creation grammar                                           |
| Bad key           | 401    | `invalid_token`      | Unknown, expired, or revoked key (`INVALID_KEY`)                                |
| Read-only key     | 403    | `access_denied`      | The key was issued read-only; creating needs write                              |
| Entitlement       | 403    | `space_limit`        | The account already owns its allowed number of spaces                           |
| Name in use       | 409    | `name_taken`         | The account already has a grant with that name                                  |
| Busy              | 429    | `busy`               | The per-key limiter refused; retry in a minute                                  |
| No capacity       | 503    | `pool_empty`         | The bucket pool has no free bucket; retryable                                   |
| Wrong method      | 405    | `method_not_allowed` | Any method other than `GET` or `POST` on the path                               |

`CliLogins.create(key, name)` owns the route's policy: it refuses `key.readOnly`
before any write, calls `Accounts.createSpace(key.principalId, name)`, which is
the one creation-grammar check and answers `ok`, `invalid`, `taken`, `limit`, or
`pool_empty`; the route maps those, marks the key used through the same path
`spaces` uses, and returns `{name, owner, access: "write"}`. `Accounts.createSpace`
already records `space_created`; the route adds no second event. The owner is
the account email, or the principal id when the account has none.

### CLI: `space`

| Invocation         | Result                                                                                                                     |
| ------------------ | -------------------------------------------------------------------------------------------------------------------------- |
| `space`            | List the targets of every reachable backend, with their directories and the default marked. On a terminal, open the picker |
| `space <name>`     | Check the name is in the listing, then store it as that identity's default: a hosted space in `credentials.json`, an S3 bucket by store key in `settings.defaults` |
| `space new <name>` | Create the target on the resolved backend, print it, and leave the default unchanged                                       |

`new` is a positional word, not a flag. A target named `new` stays
selectable: `space new` on its own sets that target as the default when a
listing contains it, and otherwise refuses and names the target to create.
`space new new` creates a target named `new`, so no valid name is unreachable.

The picker's create row runs the create path and then selects the new target,
because choosing is what the picker is for.

An S3 create addresses the identity the flags select: the current credential
chain, or the profile and endpoint that `--profile` and `--endpoint` name.

| Setting    | Flag           | Environment                                                                       | Meaning                                                       |
| ---------- | -------------- | --------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| Backend    | `--storage`    | `SLIVINGDOC_STORAGE`                                                              | `auto`, `hosted`, or `s3`; `auto` keeps the existing rules    |
| Profile    | `--profile`    | `AWS_PROFILE`                                                                     | The AWS shared-config profile; S3 only                        |
| Endpoint   | `--endpoint`   | `SLIVINGDOC_ENDPOINT` (hosted), `AWS_ENDPOINT_URL_S3` and `AWS_ENDPOINT_URL` (S3) | Backend-relative: the hosted storage endpoint whose login is used, the S3 endpoint for S3; the value a group header prints and `--endpoint` matches |
| Region     | `--region`     | `AWS_REGION`, `AWS_DEFAULT_REGION`                                                | S3 only; a profile's region wins when neither names one       |
| Prefix     | `--prefix`     | `SLIVINGDOC_PREFIX`                                                               | The prefix stored with an S3 default; empty when unset; refused with `--storage hosted` and by `space new` |
| Path style | `--path-style` | `SLIVINGDOC_PATH_STYLE`                                                           | S3 only; a custom endpoint already implies it                 |

In `auto`, an endpoint that matches a stored login selects hosted, because
that is unambiguous evidence; otherwise it is an S3 endpoint. `--profile`
counts as an S3 signal for the existing ambiguity refusal. `space` ignores
`SLIVINGDOC_TOKEN`: it needs an account login, because a token reaches one
space and cannot list or create one. With no stored login it has no hosted
group; a create, or `--storage hosted`, names `slivingdoc login`.

A hosted listing covers every stored login. A hosted create needs exactly one:
`--endpoint` chooses it, and several logins without an endpoint is a refusal
naming `--endpoint`. Each group prints one header line: the storage endpoint and
account for hosted, the endpoint, region, profile, and access key id for S3. A
hosted header's endpoint and an S3 header's endpoint and profile are the values
`--endpoint` and `--profile` accept; `(AWS)` and `(ambient)` are display tokens
for neither. The site that issued a login belongs to `slivingdoc login`'s
report, not to this header. A row is the name followed by its directories in
parentheses, and the default is marked `*`.

`space <name>` sets the default of the one identity whose listing contains the
name. When more than one identity lists it, the command refuses and names the
selector flags that differ: `--endpoint` for hosted; `--profile`, `--region`,
and/or `--endpoint` for S3. Each S3 candidate lists its endpoint, region, and
profile, so identities that differ only by region remain distinct. The refusal
lists storage endpoints, never sites, so each endpoint can be passed to
`--endpoint` unchanged. A picker row carries its identity, so the interactive
path never guesses.

```text
$ slivingdoc space analytics
error: space: analytics is listed by 2 identities (endpoint=(AWS) region=eu-north-1 profile=dev, endpoint=(AWS) region=us-east-1 profile=dev); pass --region to choose one
```

`--storage` restricts the listing: `hosted` lists only hosted, `s3` only S3,
and `auto` lists every reachable backend. In `auto`, an S3 group appears only
when an S3 signal exists or a record or default names an S3 target, so a
hosted-only machine never probes a credential chain. A `--storage` choice, or
a flag that names the S3 identity, is explicit: its failure refuses the
command. An identity that an environment variable selected under `auto`
reports its hint and does not hide the others. When no backend is
reachable at all, `space` refuses and names both fixes: `slivingdoc login`, and
an AWS profile or endpoint. A record whose hosted target has no stored login is
shown with a `space` hint of its own — `! this directory remembers hosted space
"x", which no stored login reaches; run 'slivingdoc login'` — never silently
dropped. It is not `noCredentialRefusal`, whose text names `SLIVINGDOC_TOKEN`
and belongs to `serve`, `pull` and `commit`.

`--profile`, `--region`, `--path-style`, and `--prefix` are refused when the
backend resolves to hosted, whether `--storage hosted` named it or `auto` did,
because they name an S3 store. A settings file this build cannot read refuses
a `space` listing or `space <name>` set, because the directory column cannot be
trusted. A `space new <name>` create never reads that file.

`space <name>` on S3 stores the resolved store identity and the prefix the
command resolved (`--prefix`, or empty); it does not inherit a directory's
prefix. Groups are ordered hosted first, in the credentials file's login
order, then S3, with the current chain first and recorded profiles after it.
An S3 record decorates only its exact identity: an empty endpoint is the
provider default, an empty profile the ambient chain, and an empty region
`us-east-1`; no empty field is a wildcard. Only a row the listing returned
can become a default, in the plain path and the picker alike.

### Naming

One grammar creates on both backends and on R2:

```text
3 to 63 characters
lowercase ASCII letters, digits, and inner hyphens
starts and ends with a letter or a digit
not an S3 reserved name: no xn-- or sthree- prefix, and no -s3alias,
  --ol-s3, --x-s3, or --table-s3 suffix
```

`storage.ValidateNewSpaceName` implements it, and `cloud:` mirrors it in
`validNewSpaceName`. Creation is the only place it applies. Listing, selecting,
and minting keep the existing hosted accept grammar (1 to 63 characters), so an
existing short hosted space and a legacy bucket with dots stay usable. A dotted
bucket selected from the listing records `pathStyle=true`, because a dotted name
requires path addressing on the default AWS endpoint.

### Output

The plain form below is the contract: it is what a pipe, a redirect, or
`NO_COLOR` shows. The styled form may lay the same fields out as columns and
add marks, bold, and color; it never adds a fact the plain form omits. stdout
carries the listing and success lines; stderr carries hints,
prompts, and the one-line refusal. A successful list, a create, and a set exit
zero. A degraded list, where one identity may not list, also exits zero,
because the configuration is valid. A refusal exits nonzero as
`error: <message>`.

```text
$ slivingdoc space
HOSTED  https://api.slivingdoc.dev  ada@example.com
  personal (/home/ada/journal, /srv/notes, /opt/notes, +2)  write  ada@example.com
* team-notes (/home/ada/notes, /tmp/abcd)  write  ada@example.com

S3  endpoint=(AWS)  region=eu-north-1  profile=dev  key=AKIAIOSFODNN7EXAMPLE
  analytics (/home/ada/archive)
```

A group prints one header. A `*` marks that identity's default. A hosted row
adds its access and owner; an S3 row needs neither, because the header already
names the account. A row shows at most three directories, then `+N`; the
directories of one target appear in settings-file order, and a target that no
directory remembers prints its name alone, with no parentheses. A recorded
or default target that the listing did not return is marked `!` before its
name, and the reason is a hint on stderr. A hint that reports a degraded or
unreachable identity begins with a literal `! ` in plain and styled output: it
is printed text, not a `tui` mark, because a plain mark prints nothing. The
no-default hint keeps its existing text and no mark. A row the listing did not
return prints its name and directories only: its access and owner are unknown,
and the `!` and its hint say why. A group whose listing failed and that records
nothing prints its header and its hint, never the `No spaces ...` line, which
would claim an empty store.

```text
$ slivingdoc space
HOSTED  https://api.slivingdoc.dev  ada@example.com
No spaces yet; run 'slivingdoc space new <name>' to create one.

S3  endpoint=(AWS)  region=eu-north-1  profile=dev  key=AKIAIOSFODNN7EXAMPLE
No buckets; run 'slivingdoc space new <name>' to create one.
```

A list that one identity may not perform is not a failure. Its configured
default is shown, and the hint goes to stderr:

```text
$ slivingdoc space --storage s3 --profile dev
S3  endpoint=(AWS)  region=eu-north-1  profile=dev  key=AKIAIOSFODNN7EXAMPLE
*! analytics (/home/ada/archive)
! this login may not list buckets (s3:ListAllMyBuckets is denied); the configured default is shown
```

Create never changes the default:

```text
$ slivingdoc space new research
Created hosted space research (write, ada@example.com) at https://api.slivingdoc.dev.
It is not the default; run 'slivingdoc space research' to make it the default.
```

```text
$ slivingdoc space --storage s3 --profile dev new analytics
Created bucket analytics (endpoint=(AWS), region=eu-north-1, profile=dev).
It is not the default; run 'slivingdoc space --profile dev analytics' to make it the default.
```

Selecting stores the default for that identity:

```text
$ slivingdoc space research
The default space is now research (write, ada@example.com).
```

```text
$ slivingdoc space --profile dev analytics
The default bucket for s3 endpoint=(AWS), region=eu-north-1, profile=dev is now analytics.
```

The picker (a sketch; Phase 7 owns its exact rendering):

```text
$ slivingdoc space
Pick the default target
  one row per target: identity, name, directories, access, owner, default
  one "+ Create a new hosted space" row after the hosted rows
  one "+ Create a new S3 bucket" row after the S3 rows
> 2
Name for the new hosted space: research
Created hosted space research (write, ada@example.com) at https://api.slivingdoc.dev.
The default space is now research (write, ada@example.com).
```

Refusals, one line each on stderr:

```text
$ slivingdoc space new team-notes
error: space new: team-notes is already used by this account; run 'slivingdoc space team-notes' to use it

$ slivingdoc space new extra
error: space new: this account already owns its allowed number of spaces; remove one on the site or ask for a larger plan

$ slivingdoc space new team-notes
error: space new: this login is read-only and cannot create a space; run 'slivingdoc login' and approve write access

$ slivingdoc space team-notes
error: space: team-notes is listed by 2 logins (https://api.slivingdoc.dev, https://api.dev.slivingdoc.dev); pass --endpoint to choose one

$ slivingdoc space --storage hosted team-notes
error: space: --storage hosted needs a stored login; run 'slivingdoc login'

$ slivingdoc space --storage s3 --profile nope
error: space: no AWS profile named nope; check ~/.aws/config

$ slivingdoc space new x
error: space new: storage: invalid new space name: "x" must be 3 to 63 lowercase letters, digits, or inner hyphens

$ slivingdoc space --storage s3 --profile dev new analytics
error: space new: bucket analytics already exists and this account owns it; run 'slivingdoc space --profile dev analytics' to use it

$ slivingdoc space --storage s3 --profile dev new analytics
error: space new: bucket analytics already exists in another account; choose another name

$ slivingdoc space
error: space: no backend is reachable; run 'slivingdoc login' for hosted storage, or set an AWS profile or endpoint for S3
```

## Data model additions

### `workspaces.json` version 2

```json
{
  "version": 2,
  "entries": [
    {
      "path": "/home/ada/notes",
      "target": {
        "kind": "hosted",
        "endpoint": "",
        "region": "",
        "profile": "",
        "name": "team-notes",
        "prefix": "personal",
        "pathStyle": false
      }
    },
    {
      "path": "/home/ada/archive",
      "target": {
        "kind": "s3",
        "endpoint": "https://acc.r2.cloudflarestorage.com",
        "region": "auto",
        "profile": "r2",
        "name": "team-notes",
        "prefix": "",
        "pathStyle": true
      }
    }
  ],
  "defaults": [
    {
      "kind": "s3",
      "endpoint": "",
      "region": "eu-north-1",
      "profile": "prod",
      "name": "team-notes",
      "prefix": "",
      "pathStyle": false
    }
  ]
}
```

| Field       | Kind             | Rule                                                                                                                                 |
| ----------- | ---------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| `kind`      | `hosted` or `s3` | Required. A value this build does not know is malformed                                                                              |
| `endpoint`  | string           | The effective normalized endpoint. Empty means the provider default. In this build, hosted stores an empty endpoint, and a version-1 record reads as an empty endpoint; the sibling worklog [26-10-18-directory-bindings](../26-10-18-directory-bindings/README.md) stores the effective site |
| `region`    | string           | S3 only. Empty for hosted                                                                                                            |
| `profile`   | string           | S3 only. The selector; empty means the ambient chain. Hosted leaves it empty                                                         |
| `name`      | string           | The hosted space or the S3 bucket. Replaces version 1's `space`                                                                      |
| `prefix`    | string           | The notebook prefix inside the store; validated by `storage.ValidatePrefix`                                                          |
| `pathStyle` | bool             | S3 only. The effective addressing mode. Hosted leaves it false                                                                       |

Rules that bind the file:

- **Version 2 reads version 1 and writes version 2.** A version-1 entry reads
  as `{kind:"hosted", endpoint:"", name:<space>, prefix:<prefix>}`: the empty
  endpoint means the selected login's endpoint, today's behavior. Reading never
  rewrites. The next successful write stores that same hosted target in the
  version-2 shape; storing the effective endpoint belongs to the sibling
  worklog.
- **`defaults` holds S3 targets only in this build.** A `hosted` target in
  `defaults` is malformed, because a hosted default lives in
  `credentials.json` (`defaultSpaces`), and two sources must not disagree.
- **`defaults` is present in every version-2 file**: an empty array when none
  is stored. The encoder writes it and the decoder requires it.
- **A default is looked up by the store key** `{kind, endpoint, region,
  profile}`. The record repeats the fields so a listing can group rows without
  resolving credentials.
- **A hosted target with a non-empty `region`, `profile`, or `pathStyle` is
  malformed.** A field is either meaningful for the kind or absent; nothing is
  silently ignored.
- **Strict decoding is unchanged:** unknown, duplicate, missing, and null
  members are refused; a version this build does not read is
  `ErrUnsupportedVersion` with the existing newer-build message.
- **The file, lock, bound, and permissions are unchanged:**
  `<config-dir>/slivingdoc/workspaces.json`, `workspaces.lock`, 1 MiB,
  `0600`/`0700`, hardened through `internal/credentials`.
- **The record never holds a credential.** It holds a profile name, which is
  not a secret.
- **Entries are mutable; the file is replaced atomically.** A `Put` replaces
  the entry with the same path where it stands, appends a new path, and keeps
  every other entry, under `workspaces.lock`.

`credentials.json` is unchanged: logins and the per-endpoint hosted default
space stay where they are.

This worklog changes no notebook identity, no private-state key, no shared
cache directory, and no probe proof record. Those follow from how a command
resolves a target, and belong to `26-10-18-directory-bindings`.

## Status board

| Phase                                                                    | Status      | Outcome                                                                                                                           |
| ------------------------------------------------------------------------ | ----------- | --------------------------------------------------------------------------------------------------------------------------------- |
| [1. Settings version 2](phase-1-settings-v2.md)                          | Complete (2026-10-19, session 1) | A backend-aware target, S3 defaults keyed by store key, and a version-1 read path that never rewrites                             |
| [2. Cloud create route](phase-2-cloud-create-route.md)                   | Complete (2026-10-19, session 1) | `POST /cli/v1/spaces`, `CliLogins.create`, the portable name validator, and the cloud tests                                       |
| [3. `sitelogin.CreateSpace`](phase-3-sitelogin-create-space.md)          | Complete (2026-10-19, session 2) | The client call, a reference-site route, and a `2xx` body in `call`                                                               |
| [4. Hosted `space` backend and `space new`](phase-4-space-new-hosted.md) | Complete (2026-10-19, session 3) | The backend interface, the `space` flag set, hosted create and default, directories in rows, and the rule that a token is ignored; R2-04, R2-06 and R2-07 closed in session 14; R3-01 and R3-02 evidence tests passed in session 17 |
| [5. S3 admin and profile](phase-5-s3-admin-and-profile.md)               | Complete (2026-10-19, session 4) | `ListBuckets`, `CreateBucket`, the resolved identity, `--profile`, and effective endpoint, region, and path style; R2-08 doc note closed in session 15 |
| [6. S3 `space` backend](phase-6-space-s3.md)                             | Complete (2026-10-20, session 12) | R2-01, R2-02, R2-03 and R2-10 remediation verified; `make qa` passed with 89.3% coverage and 35 npm tests, and dupl triage found only acceptable clones; R3-03 specification wording corrected in session 18 |
| [7. Interactive picker](phase-7-space-picker.md)                         | Complete (2026-10-20, session 13) | R2-02 and R2-05 verified/fixed; focused tests and dupl triage passed; coordinator `make qa` passed with 89.4% coverage and 35 npm tests |
| [8. Quality gate](phase-8-quality-gate.md)                               | Complete (2026-10-20, session 8) | Both repositories green, every architecture doc this worklog touches true, and the readiness checklist re-verified; R2-09 is a process note |
| Post-close review remediation                                         | Complete (2026-10-20, session 10) | Exact S3 identity matching, reachable-only defaults, environment-selected identity degradation, help and docs corrected |
| Post-close review round 2                                             | Complete (2026-10-20, session 15) | All ten findings resolved across Phases 4, 5, 6 and 7; R2-08 doc note closed |
| Post-close review round 3                                             | Complete (2026-10-20, session 18) | Three minor findings; R3-01 and R3-02 test evidence closed in session 17, R3-03 phase-specification wording closed in session 18; no phase reopened |

Phases complete in numeric order. All eight phases are complete, and session
10 resolved every finding of the first post-close review. Sessions 12 through
15 resolved the review-round-2 findings assigned to Phases 6, 7, 4 and 5.
Every review-round-2 finding is resolved. Session 17 closed R3-01 and R3-02,
and session 18 closed R3-03; every review-round-3 finding is resolved. The
full quality gate passed after the Phase 7 edits; session 14 reran it after the
Phase 4 documentation fixes, session 15 closed the Phase 5 documentation note,
and the final close-out run exited zero with 89.3 % coverage and 35 npm tests.
The Feedback index records the fixes and the Status board marks each phase.
Phase 1 precedes 4 and 6. Phase 3 precedes 4. Phase 5 is independent of
Phases 2 to 4 and precedes 6.
Phase 6 needs Phases 1, 4, and 5. Phase 7 needs Phases 4 and 6. Phase 8 depends
on all of them.

There is no gate phase. The in-repo reference site and the SeaweedFS test
container prove the two backends; no claim here rests on an unverified
external service. R2 appears as a configuration example only and is not
advertised as supported until a live probe passes.

## Scope

**In scope.** The two `space` backends; listing across every stored hosted
login and every reachable S3 identity; creation through the hosted route and
the S3 API; the store-key default; the profile selector; the effective
endpoint, region, and path style read back from the AWS configuration; the
backend-aware target type and the settings file version 2; the operator surface
and the architecture docs for all of it.

**Out of scope. No phase does these:**

- No change to the MCP tools, the tool schemas, or the one-directory server.
  That is `26-10-18-directory-bindings`.
- No change to how `serve`, `pull`, `commit`, `status`, or `log` resolve a
  target, and no directory record is written by this worklog. `space` reads
  records for the directory column only. Two exceptions reach the other
  commands: the new `--profile` flag, which `serve`, `pull`, `commit`, `status`,
  and `log` accept and pass to the S3 store, and the empty `--region` default
  with its precedence, which every command shares.
- No change to `workspace.Identity`, the probe proof record, or the private
  state layout.
- No environment-file feature. The AWS shared configuration is the credential
  source, and slivingdoc never reads it itself.
- No enumeration of every profile in `~/.aws`. A listing covers the current
  chain and the profiles that a record or default already names.
- No S3 credential storage, no key prompt, and no secret display. The access
  key id is informational in `space` output only; it is never stored, logged,
  or returned over MCP.
- No bucket configuration: no policy, versioning, encryption, lifecycle, or
  region selection beyond the one a create needs.
- No implicit creation anywhere. `s3store.New` and the probe never create a
  bucket; a missing bucket is still the existing refusal.
- No `encoding/json/v2` migration. The strict wire-struct pattern stays.
- No claim that R2 works before a probe passes, and no live cloud or AWS
  endpoint in any test.
- No change to the notebook protocol, merge, checkpoints, cleanup, or the
  error taxonomy's categories.
- No cloud deployment, and no infrastructure or pool change. Phases produce
  code and tests; a person deploys.
- No renaming or deleting of spaces or buckets, and no new MCP tool.
- No command removes a directory record; this worklog never writes one.

## Shared interfaces

```go
// internal/settings
type Kind string // KindHosted, KindS3

type Target struct {
    Kind      Kind
    Endpoint  string
    Region    string
    Profile   string
    Name      string
    Prefix    string
    PathStyle bool
}

type StoreKey struct {
    Kind     Kind
    Endpoint string
    Region   string
    Profile  string
}

type Entry struct {
    Path   string
    Target Target
}

func (t Target) Key() StoreKey
func (s Set) Entries() []Entry
func (s Set) Lookup(path string) (Target, bool)
func (s Set) Put(Entry) Set
func (s Set) Defaults() []Target
func (s Set) Default(StoreKey) (Target, bool)
func (s Set) PutDefault(Target) Set

// internal/storage
func ValidateNewSpaceName(name string) error // the portable creation grammar

// internal/app
type SpaceHeader struct {
    Kind     settings.Kind
    Endpoint string // effective, empty for the provider default
    Region   string
    Profile  string
    KeyID    string // S3 only, display only
    Account  string // hosted only
}

type SpaceRow struct {
    Name        string
    Owner       string
    Access      credentials.Access
    Directories []string
    Reachable   bool // false for a recorded or default target the listing did not return
}

type spaceBackend interface {
    Kind() settings.Kind
    Header() SpaceHeader
    List(ctx context.Context) ([]SpaceRow, error)
    Create(ctx context.Context, name string) (SpaceRow, error)
    Default() (settings.Target, bool)
    SetDefault(ctx context.Context, name string) error
}

// internal/sitelogin
type Space struct{ Name, Owner string; Access credentials.Access }
func (c *Client) CreateSpace(ctx context.Context, key, name string) (Space, error)

// internal/s3store
type Bucket struct{ Name string; Created time.Time }
type Identity struct{ Profile, AccessKeyID, Endpoint, Region string }
type Resolved struct{ Profile, Endpoint, Region string; PathStyle bool }

// Resolve loads the AWS configuration without a bucket and without
// retrieving credentials.
func Resolve(ctx context.Context, cfg Config, opts ...Options) (Resolved, error)
func (s *Store) Resolved() Resolved

type Admin struct{ /* unexported: the SDK client and the resolved config */ }
// NewAdmin ignores Config.Bucket and Config.Prefix.
func NewAdmin(ctx context.Context, cfg Config, opts ...Options) (*Admin, error)
func (a *Admin) ListBuckets(ctx context.Context) ([]Bucket, error)
func (a *Admin) CreateBucket(ctx context.Context, name string) error
func (a *Admin) Identity(ctx context.Context) (Identity, error)

var ErrBucketExists error // exists under another account or principal
var ErrBucketOwned error  // exists and this principal already owns it
```

`--profile` sits on `app.Flags` and `realStoreFactory` passes it to
`s3store.Config.Profile`; this worklog introduces both and nothing layers a
record over them. `Resolve` and `New` share one loader; `Resolve` returns the
effective values without a bucket, a client, or a credential retrieval.
`Store.Resolved` reports the values the store was built with.

The `space` command owns the backend implementations. `sitelogin` owns the
hosted call. `s3store` owns every AWS SDK type, including `Admin`. `settings`
owns the file and the store key. No package outside `internal/workspace` reads
a notebook identity, and this worklog never reads one.

## Invariants

A path that is not a row does not exist; a phase that needs one adds a row
first. Each row names its proving phase; the phase names the test.

| #   | Invariant                                                                          | Actors                     | Mechanism                                                          | Proof          |
| --- | ---------------------------------------------------------------------------------- | -------------------------- | ------------------------------------------------------------------ | -------------- |
| 1 | The settings file holds no credential                                              | `internal/settings`        | `Target` has no secret field; the profile is a name; strict codec  | Phase 1        |
| 2 | One creation grammar, mirrored in both repositories                                | `internal/storage`, cloud: | `ValidateNewSpaceName` and `validNewSpaceName` are the same rule over the same name vectors, and the reference site keeps one test-only copy for its own routes | Phases 2 and 3 |
| 3 | Creation is always explicit                                                        | `s3store`, `space`         | Only `CreateBucket` and `POST /cli/v1/spaces` create; `New` never  | Phase 5: `TestAdminNeverCreates` |
| 4 | A default is stored and found by its exact store key                              | `internal/settings`        | `Target.Key()`, `Set.PutDefault`, `Set.Default`                     | Phase 1        |
| 5 | The access key id is display-only                                                  | `space`                    | Not stored, not logged, not returned over MCP                       | Phases 5 and 6 |
| 6 | A record may add a listing group and its directory, but never selects the target of a create or set-default; a create and a default always come from flags and environment | `internal/app` | Records and defaults can add listing groups; flags and environment select create and set-default targets | Phase 4: `TestSpaceNeverTargetsARecord`; Phase 6: `TestSpaceS3Directories`, `TestSpaceS3DegradedList` |
| 7 | A recorded or default target the listing did not return stays visible             | `internal/app`, `tui`      | `SpaceRow.Reachable` false and the `!` marker                       | Phases 4, 6, 7 |
| 8 | A recorded S3 field is resolved by its stated value, so a record never collapses into another identity | `internal/app` | A recorded target resolves with `regionDefault(key.Region)`, so an empty region is `us-east-1` and the resolved key cannot alias the current identity | Phase 6 (remediation): `TestSpaceS3RecordWithoutRegion` |
| 9 | Every creation path applies the portable creation grammar | `space` | `storage.ValidateNewSpaceName` runs before any backend `Create`, on the plain path and in the picker | Phases 4, 6 and 7 (remediation): `TestSpacePickerRefusesAnInvalidS3Name` |

## Parameters and owners

| Parameter                        | Value                                                                                                                       | Owner                                | Test                                     |
| -------------------------------- | --------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ | ---------------------------------------- |
| Portable creation grammar        | 3–63 characters, lowercase letters, digits, inner hyphens, alphanumeric ends, S3 reserved prefixes and suffixes refused     | Phase 3 (Go owner); Phase 2 mirrors it | Table tests in both repositories, over the same vectors |
| Directory display in a space row | At most three paths, then `+N`                                                                                              | Phase 4 (consumed by Phases 6 and 7) | `TestSpaceRowsShowThreeDirectories`, picker row tests |
| S3 bucket creation configuration | `CreateBucketConfiguration` only for the default AWS endpoint when the effective region is not `us-east-1`                  | Phase 5                              | `TestCreateBucketBody`                   |
| Cloud create limiter             | The existing per-key limiter, key `create:<key id>`                                                                          | Phase 2                              | `rate limits creation per key`           |
| Access key id display            | The full identifier, `space` stdout only                                                                                     | Phase 6                              | `TestSpaceS3Header`, `TestSpaceS3KeyIDIsDisplayOnly` |
| Settings file bound              | 1 MiB, unchanged                                                                                                            | Existing                             | Existing tests                           |
| `--profile` on the shared flags  | Flag `--profile`, environment `AWS_PROFILE`, default empty; a selector, never a secret                                       | Phase 5                              | `TestProfileFlag`                        |
| Region precedence                | Flag `--region` with an empty default, then `AWS_REGION`, then `AWS_DEFAULT_REGION`; a profile region wins when none names one; `us-east-1` is the S3 default | Phase 5 | `TestRegionDefaultIsEmpty`, `TestRegionReadsAWSDefaultRegion`, `TestProfileRegionWins` |
| `--profile` on `SpaceFlags`      | The same value, bound like the other `space` flags; refused when the backend resolves to hosted                          | Phase 4                              | `TestSpaceHostedRefusesS3Flags`          |
| `space` flag set                 | `--storage`, `--endpoint`, `--region`, `--path-style`, and `--prefix`, bound like the shared flags                          | Phase 4                              | `TestSpaceHostedRefusesS3Flags`, `TestSpaceUsageNamesTheModes` |
| S3 identity surface              | `s3store.Config.Profile`, `s3store.Resolve`, `Store.Resolved`, and `Admin`                                                   | Phase 5                              | `TestProfileSelectsTheSharedSection`, `TestResolveReportsEffectiveValues` |
| S3 admin injectable seam         | The unexported `s3Admin` factory on `ProcessOptions`, test-only; `Resolve` and `NewAdmin` build the production value         | Phase 6                              | `TestSpaceS3UsesListBucketsOnly`, `TestSpaceS3CreateUsesCurrentIdentity` |
| Plain output parity (picker)     | The picker adds no fact and changes no text of the plain form                                                                | Phase 7                              | `TestSpacePlainUnchanged`                |
| `--prefix` on an S3 target       | Flag `--prefix`, environment `SLIVINGDOC_PREFIX`, empty for `space`; validated by `storage.ValidatePrefix`; refused on a hosted backend and by `space new` | Phase 4 (binding and hosted refusal); Phase 6 (S3 validation and create refusal) | `TestSpaceHostedRefusesS3Flags`, `TestSpaceS3InvalidPrefix`, `TestSpaceS3CreateRefusesPrefix` |
| Settings types                   | `settings.Kind`, `Target`, `StoreKey`, `Set.Default`, `Set.PutDefault`                                                      | Phase 1                              | `TestV2RoundTrip`, `TestDefaultByStoreKey` |
| Plain output wording             | The examples under Output                                                                                                   | Phase 4 (consumed by Phases 6 and 7) | `TestScenarioSpaceHosted`, `TestScenarioSpaceS3` |

Every phase-owned default, limit, and flag is introduced by the phase in this
table and tested there. The README owns only the shared values above. The
default lookup key `{kind, endpoint, region, profile}` is stored here and
completed before a lookup by the sibling worklog.

## Definition of success

- `slivingdoc space` lists every stored hosted login's spaces and every S3
  bucket reachable through the current chain or a recorded profile, with the
  directories that remember each target and the default marked. One failing
  backend never hides another.
- `slivingdoc space new team-notes` creates a hosted space through
  `POST /cli/v1/spaces` against the reference site, and creates an S3 bucket
  against the test container, then prints the target.
- `slivingdoc space team-notes` stores the default for the resolved store key.
- A space or bucket that the listing did not return, but a record or default
  names, is still shown, marked unreachable.
- Both repositories' gates are green and every architecture doc this worklog
  touches is true.

## Validation policy

- Public repository: `make qa` runs lint (`gofumpt`, `go vet`, `staticcheck`,
  `go fix -diff`), the race test with `-count=3` and the 70 % coverage floor,
  and the npm tests.
- After `make qa`, run `go run github.com/mibk/dupl@v1.0.0 -t 80 .` as a
  separate signal and triage its findings.
- Cloud repository: `make qa` is the whole gate. It runs `cloud` (the storage
  gateway's typecheck and tests) as a prerequisite, then the site's lint,
  build, fixtures, tests, links, and dupl. A phase runs `make cloud` alone only
  to reproduce a gateway failure.
- The cloud end-to-end suite is not part of `make qa`; it runs a stack. A
  phase that changes a CLI login flow updates its spec as evidence, and a
  local run is recorded in that phase.
- Integration scenarios in `internal/integrationtest` are the behavioral
  contract of the public command surface. No phase adds a skip, a short mode,
  or a second gate.
- The S3 tests use the pinned test container through `internal/tests3`. No
  test reaches AWS, R2, or any live cloud endpoint.

## Readiness checklist

1. The shared diagrams explain the `space` list and create surfaces; phase-local
   sequences appear only in their phase.
2. Every test has one owning phase and a file list; no phase redeclares
   another's test.
3. Every parameter, flag, injectable field, invariant, and limit has one owner
   and a test; shared values appear once above.
4. No phase needs a person: the cloud checkout, its dependencies, the
   Playwright browsers, Docker, and the pinned libgit2 are present, so no phase
   declares `Human required`. No phase takes a credential or listens for a
   human.
5. No phase depends on local detail from another phase or on text scheduled
   for deletion.
6. No phase creates human-owned configuration: no AWS profile, no cloud
   deployment, no bucket pool entry, no site setting.
7. The conventions cited are the ones the code uses: the domain-plus-wire
   structs of `internal/settings` and `internal/credentials`, the flag,
   environment, and default order of `internal/app`, and the SDK containment
   of `internal/s3store`.
8. A phase that changes a shared value lists every doc sentence that states the
   old value in its `Documentation` table, and proves each is gone with a
   check.

## Feedback index

Validation round 1 (2026-10-18). Every finding is closed in place; no contract
was redesigned, and each phase stays Not Started.

| ID    | Severity | Finding                                                                                | Closing edit                                                                                                     |
| ----- | -------- | -------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| V1-01 | major    | No phase named the files that hold its tests, against readiness item 2                 | A `Files` table closes each phase's Specification in Phases 1 to 7; readiness item 2 says where                  |
| V1-02 | major    | README invariants 3 and 6 named no test in their proof phase                           | `TestAdminNeverCreates` (Phase 5), `TestSpaceNeverTargetsARecord` (Phase 4), and the README proof cells           |
| V1-03 | major    | The picker PTY criteria had no harness that can produce a terminal                     | Phase 7 `Terminal harness` with `runCLIOnTypedTTY`, and the Phase 8 wording                                      |
| V1-04 | major    | The rendering and picker helpers shared with `login` were not reconciled               | Phase 4 `Shared rendering and picker helpers`, `TestLoginOutputUnchanged`, and the Phase 7 note                  |
| V1-05 | major    | Doc updates were deferred to the gate phase, and the cloud route named no doc          | `Documentation` tables in Phases 1 to 7; Phase 8 verifies them by owner                                           |
| V1-06 | major    | The `endpoint` field rule contradicted Phase 1 and the sibling boundary                | README field row                                                                                                 |
| V1-07 | minor    | A broken `admin.EffectiveEndpoint` reference and an authoring artifact survived        | Phase 5 `Resolve` sentence and integration row                                                                   |
| V1-08 | minor    | The parameters table gave the directory display and the plain wording to the wrong phases | README `Parameters and owners`                                                                                |
| V1-09 | minor    | `--prefix` was missing from the synopses and undefined with `--storage hosted`         | README synopses, flag table, and hosted refusal; Phase 4 refusal rows                                            |
| V1-10 | minor    | The table said `space` gains flags that exist, and `--profile` had two owners          | README parameter rows; Phase 4 `Command surface`                                                                 |
| V1-11 | minor    | The environment spellings of an S3 signal were unstated                                | Phase 4 `Backend selection`                                                                                      |
| V1-12 | minor    | The `call` table changed the `out == nil` bound case without saying so                 | Phase 3 `call` table and `TestCallRejectsOversized2xxWithoutBody`                                                |
| V1-13 | minor    | Provenance linked a worklog directory that no longer exists                            | README provenance                                                                                                |
| V1-14 | minor    | No phase owned the usage text                                                          | Phase 4 `Usage text` and `TestSpaceUsageNamesTheModes`                                                           |
| V1-15 | note     | One parameter row bundled three phases of flags and types                              | README `Parameters and owners` rows                                                                              |
| V1-16 | note     | The grammar mirror was asserted without a shared vector set                            | README invariant 2, Phase 2, and Phase 3                                                                         |

Validation round 2 (2026-10-18). Each round-1 finding was re-checked against
its cited edit: V1-01 and V1-03 to V1-16 are resolved; V1-02 stayed open when
re-checked, and V2-09 closes it. Six majors, eight minors, and four notes were
filed and closed in place. No contract was redesigned, and every phase stays
Not Started.

| ID    | Severity | Finding                                                                                | Closing edit                                                                                                     |
| ----- | -------- | -------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| V2-01 | major    | `make qa` was said to run dupl, but it runs lint, test, and npm only                   | README `Validation policy` and Phase 8 `Public repository gate` name dupl as a separate signal                   |
| V2-02 | major    | Phase 4 changed the `space` plain output but owned no doc edit                          | Phase 4 `Documentation` and the Phase 8 doc table require the new listing, markers, default and refusal lines in `architecture/login.md`, and the argument forms and `Space.Run` flow in `architecture/cli.md` |
| V2-03 | major    | Phase 4 broke the existing `space` output assertions without owning them                | Phase 4 `Files`, an integration row, and an acceptance criterion own the rewritten assertions in `internal/app/login_test.go` and `internal/integrationtest/scenario_login_test.go` |
| V2-04 | major    | Phase 2 gave the creation grammar two owners and omitted `cloud/site/accounts.ts`      | `Accounts.createSpace` is the one grammar check; Phase 2 `Files` adds `cloud/site/accounts.ts` and `cloud/test/app.test.ts`; the README API paragraph follows |
| V2-05 | major    | The region environment and precedence had no owner or test                              | Phase 5 `Region resolution`, region tests in `Invariants` and `Acceptance`, `internal/app/app.go` and `storage_test.go` in `Files`, the README `Region precedence` row, and the Phase 8 `running.md` row |
| V2-06 | major    | Phase 6 stored `--prefix` without `storage.ValidatePrefix`                              | Phase 6 validates the prefix before listing or writing, stores the validated value, and adds `TestSpaceS3InvalidPrefix` |
| V2-07 | minor    | Phase 4's usage text named a nonexistent `internal/cli` `HelpText` and listed no file   | Phase 4 `Usage text` names `internal/cli.Usage` and `spaceHelp`, lists `internal/cli/cli.go` and `internal/cli/cli_test.go`, and says the assertion is new |
| V2-08 | minor    | Phases 5 and 6 omitted `internal/app/app.go`; Phase 5 omitted its app test files        | `Files` rows in Phases 5 and 6                                                                                    |
| V2-09 | minor    | Invariant 3's proof named Phase 2 without a Phase 2 test (V1-02 residue)                | README invariant 3 proof names Phase 5 only                                                                      |
| V2-10 | minor    | `cloud:API.md` resolved to a missing path                                               | Phase 2 and Phase 8 say `cloud:cloud/API.md`                                                                     |
| V2-11 | minor    | The degraded-list example showed `* analytics` although the rule marks the row `!`      | README example shows `*! analytics`                                                                              |
| V2-12 | minor    | The documented `--prefix` refusal on `space new` had no test                            | Phase 6 `Acceptance` and `Error coverage` add `TestSpaceS3CreateRefusesPrefix`                                   |
| V2-13 | minor    | Phase 4 gave an expired login and the no-login case two outcomes                        | Phase 4 `Error coverage` splits the expired listing from the create refusal and scopes the no-login case to `--storage hosted` |
| V2-14 | minor    | Phase 3 acceptance named no tests, and Phase 2 said `at or over` the body bound         | Phase 3 names the `sitetest` tests and an `rg` check; Phase 2 says `over`                                        |
| V2-15 | note     | The scope omitted `status` and `log` from the shared `--profile` flags                  | README `Scope`                                                                                                   |
| V2-16 | note     | The boundary sentence gave the effective values to the sibling, and the S3 flow said `ListBuckets` was the only call | README boundary paragraph and S3 flow                                                                            |
| V2-17 | note     | The glossary omitted the hosted default, and five parameter rows named no test          | README `Glossary` and `Parameters and owners`                                                                    |
| V2-18 | note     | Phase 7 shared `Login.confirm`'s reader without a file row, and `pickable()` omitted stdout; Phase 4 left its terminal path unstated | Phase 7 `Terminal harness` and `Files`; Phase 4 picker-helper note                                                |

Validation round 3 (2026-10-18). Each round-2 finding was re-checked against
its cited edit: all resolved, none regressed. The round was then re-scored
against one rule: a major is a contract violation that survives a competent
phase worker and a green gate. Two are majors; the rest are minors and notes.
No contract was redesigned, and every phase stays Not Started.

| ID    | Severity | Finding                                                                                | Closing edit                                                                                                     |
| ----- | -------- | -------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------- |
| V3-01 | minor    | Phase 5's empty region default would meet the `region is required` refusal in `config.finish` | Phase 5 `Region resolution`, the `config.go` `Files` row, and an invariant row                                 |
| V3-02 | major    | No phase owned the docs that state the old region order and the old signal set          | Phase 5 `Documentation` gains `config.md` and `login.md`, its `Acceptance` adds the checks, and the Phase 8 table splits ownership |
| V3-03 | major    | Hints and examples put flags after the positional word the command refuses              | README examples, Phase 6 hint and refusal text, and a Phase 6 note on flag order                                |
| V3-04 | minor    | A stored hosted default absent from a successful listing had no `!` row                 | Phase 4 `Hosted listing`, `Invariants`, `Acceptance`, `Error coverage` (`TestSpaceShowsMissingDefault`)          |
| V3-05 | minor    | "The plain output is exactly the README's examples" cannot pass in a hosted-only phase   | Phase 4 `Acceptance`                                                                                             |
| V3-06 | minor    | `TestSpacePlainUnchanged` had two owning phases                                         | README `Parameters and owners`                                                                                   |
| V3-07 | note     | `spaceHelp` is unexported, so `internal/cli` cannot assert it                           | No edit; the worker asserts `SpaceCommand(...).Help()`                                                           |
| V3-08 | minor    | `--prefix` had no parameter row                                                         | README `Parameters and owners`                                                                                   |
| V3-09 | note     | The orphan-record hint was called "existing" and then reworded                          | No edit; the worker reuses `noCredentialRefusal` or updates the README line                                      |
| V3-10 | minor    | The unknown-profile refusal had no test                                                 | Phase 6 `Error coverage` and `Acceptance`                                                                        |
| V3-11 | note     | Phase 4 omitted `--endpoint` as an S3 endpoint source                                   | Phase 4 `Backend selection`                                                                                      |
| V3-12 | note     | The `s3Signals` seam over `SpaceFlags` was unnamed                                      | No edit; the worker decides and lists the file                                                                   |
| V3-13 | minor    | Phase 2 did not own the cloud e2e spec that the validation policy requires              | Phase 2 `Files`, `Acceptance`, and `Human required`                                                             |
| V3-14 | note     | Phase 4's endpoint sentence duplicated Phase 6 incorrectly                              | Phase 4 `Backend selection` (same edit as V3-11)                                                                 |

Validation round 4 (2026-10-19). Each round-3 finding was re-checked against
the edit it cites: V3-01, V3-03 to V3-06, V3-08, V3-10, V3-11, V3-13 and V3-14
are resolved; V3-02, V3-07, V3-09 and V3-12 stayed open, and V4-05, V4-12,
V4-19 and V4-13 close them. Four majors, ten minors, and five notes were filed
and closed in place. The doc-falsification and executable-example classes had
already run, so each of their new instances records its class: the next round
checks the class, not the cited line.

| ID    | Severity | Class                | Finding                                                                                          | Closing edit                                                                                                 |
| ----- | -------- | -------------------- | ------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------ |
| V4-01 | major    | doc falsification    | Phase 4 falsified the `space` transcript in `architecture/running.md`, and no phase owned it      | Phase 4 `Documentation` gains the `running.md` row, an `rg -n 'owned by' architecture/running.md` acceptance check, and Phase 8 gives `running.md` to Phases 4 and 5 |
| V4-02 | major    | doc falsification    | Phase 6 shipped a user-visible S3 `space` surface and owned no doc edit                           | Phase 6 `Documentation` adds `architecture/login.md` and `architecture/cli.md`, an `rg` acceptance check, and Phase 8 owners `Phases 3, 4, 5 and 6` and `Phases 4, 6 and 7` |
| V4-03 | major    | behavior ownership   | The hosted header and the ambiguity refusal printed the site, while `--endpoint` matches the storage endpoint | README flag table, header, refusal example and listing prose; Phase 4 `Header()`, Hosted listing and Setting the default; Phase 6 identity spelling; README Output and picker examples; Phase 7 `TARGET` and create row |
| V4-04 | major    | executable example   | The picker under `ProcessOptions.interruptible` swallowed SIGINT while the table's read blocked    | Phase 7 scopes `interruptible` to the prompt read, states that Ctrl-C at the table ends the process, and adds the invariant, acceptance row, error row and `TestScenarioSpacePickerInterruptAtTable` |
| V4-05 | minor    | doc falsification    | The region checks were vacuous and missed the `… are ignored` sentence and the old default rows    | Phase 5 `running.md` row names the deleted sentence; its acceptance checks `always explicit` and `setting are ignored` with `rg -e`, the `us-east-1` rows, the `--profile` hits and the removed `region is required` |
| V4-06 | minor    | executable example   | The AWS SDK containment check could not fail (`internal/tests3` also matches)                      | Phase 5 acceptance and invariant use the `--glob '!internal/s3store/**' --glob '!internal/tests3/**'` form and say `tests3` is test-only |
| V4-07 | minor    | executable example   | The hosted create hint omitted the flag that selected the login                                   | Phase 4 Hosted create repeats `--endpoint` and `--storage hosted`, with `TestSpaceNewHintFlags`                |
| V4-08 | minor    | value trace          | The plain `!` hint prefix had no stated origin against a plain `Mark` that prints nothing          | README Output rule, Phase 4 Hosted listing and Output stream rules, Phase 6 hint, and the byte-exact hint acceptance row |
| V4-09 | minor    | behavior ownership   | A target named `new` was unselectable by `space <name>`                                           | README CLI table, Phase 4 mode table and `TestSpaceNewNamedNew`                                               |
| V4-10 | minor    | doc falsification    | The `space <name>` row named the store key for both backends, contradicting the glossary           | README CLI row states the hosted and S3 default stores separately                                             |
| V4-11 | minor    | repository fact      | `space`'s `--storage hosted` no-login refusal could reuse the token wording of `noLoginRefusal`    | Phase 4 `Backend selection` and `login.md` Documentation row; README refusal example                           |
| V4-12 | minor    | symbol reachability  | `internal/cli` was told to assert the unexported `spaceHelp`                                       | Phase 4 `Usage text` and `Files` assert `login.SpaceCommand(...).Help()` and `.Describe()`                    |
| V4-13 | minor    | repository fact      | `s3Signals` takes `*app.Flags`, so `SpaceFlags` cannot use it as written                           | Phase 4 `Backend selection` names the narrow flag-set view and adds `internal/app/storage.go` to `Files`       |
| V4-14 | note     | structure            | The shared loader list omitted the static `Config.AccessKey`/`SecretKey` provider                  | Phase 5 `One loader` names it                                                                                  |
| V4-15 | note     | repository fact      | The sibling's prerequisite needs the `space` command (Phase 4), which Provenance omitted           | README Provenance names Phases 1, 4, 5 and 6                                                                  |
| V4-16 | note     | structure            | Three phases restated the directory display bound and the field separator                          | Phases 6 and 7 refer to Phase 4's rule                                                                        |
| V4-17 | note     | repository fact      | The cloud `qa` target depends on `cloud`, so the gate ran twice                                    | README cloud note and Validation policy; Phase 8 gate, invariant, contract and acceptance                     |
| V4-18 | note     | structure            | Phase 2 `Human required` did not say what happens after the person provides the artifact           | Phase 2 `Human required` names the resume point                                                              |
| V4-19 | minor    | doc falsification    | V3-09 residue: the README called the orphan hint the existing `noCredentialRefusal` one while Phase 4 specified its own text | README listing prose gives `space`'s own `! ` hint and says why it is not `noCredentialRefusal`; Phase 4 Hosted listing repeats it |

Validation round 5 (2026-10-19). Each round-4 finding was re-checked against
the edit it cites: all nineteen are resolved and none regressed. The round ran
the readiness checklist, the evidence sweep, and the shared-value sweep against
the tree; the classes already run stayed at minor or note, so no contract was
redesigned and every phase stays Not Started.

| ID    | Severity | Class              | Finding                                                                                                      | Closing edit                                                                                                                   |
| ----- | -------- | ------------------ | ------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| V5-01 | minor    | evidence ownership | Phase 5 stops refusing an empty S3 region but did not name the existing `TestLoadConfigRegionRequired` that asserts the refusal | Phase 5 `Files` rewrites it as `TestEmptyRegionFlagIsLegal`, and `Acceptance` names that test                                    |
| V5-02 | minor    | evidence ownership | Phase 4 degrades an expired login and changes the `space` description, while `TestSpaceRefusesAnExpiredOrChangedLogin` and the styled `space -h` prefix assert the old behavior | Phase 4 `Files` names `internal/app/login_test.go`'s third test and adds `scenario_colour_linux_test.go`; `Acceptance` names the rewritten assertion |
| V5-03 | minor    | behavior ownership | The README refuses an explicitly asked backend that cannot authenticate, while Phase 4 degraded an expired login for `--storage hosted` too | Phase 4 `Error coverage` splits the `auto` hint from the `--storage hosted` refusal, `TestSpaceHostedExpiredLoginRefuses`       |
| V5-04 | minor    | doc falsification  | Phase 1's `config.md` row named no sentence that becomes false; the scoped-settings paragraph still said `version 1` and `a space and a prefix only` | Phase 1 `Documentation` names both sentences, `Acceptance` adds the `rg` check                                                  |
| V5-05 | minor    | executable example | Both README examples listed `team-notes` before `personal`, while Phases 4 and 7 sort rows by name and Phase 4 asserts the examples exactly | README Output and picker examples are sorted                                                                                   |
| V5-06 | minor    | behavior ownership | A synthesized hosted row's access and owner were undefined, and a failed group with no records printed `No spaces yet`, which claims an empty store | README Output rule, Phase 4 `Hosted listing`, and an `Acceptance` row                                                          |
| V5-07 | minor    | doc falsification  | Phase 6 asked `errors.md` for reason and action tokens on `space` refusals, which are plain CLI text outside the taxonomy | Phase 6 `errors.md` row states the taxonomy covers MCP results only                                                            |
| V5-08 | minor    | structure          | Phase 3 required a duplicate `name` refusal from the reference site without a mechanism; `decodeStrict` keeps the last member | Phase 3 `Reference site` gives `decodeStrict` the duplicate-key refusal and says no existing route sends one                    |
| V5-09 | minor    | value trace        | README `Scope` named only `--profile` as an exception to the other commands' target resolution, while Phase 5 also changes the `--region` default | README `Scope` names both exceptions                                                                                           |
| V5-10 | note     | structure          | Invariant 2 named two mirrors, but the reference site keeps a test-only third copy                            | README invariant 2 mechanism names it                                                                                          |
| V5-11 | note     | structure          | Readiness item 3 counts injectable fields, but the new `s3Admin` factory had no parameter row                 | README `Parameters and owners` gains the seam row                                                                              |

Review round 2 (2026-10-20, session 11). The first post-close review lives in
[review-2026-10-20.md](./review-2026-10-20.md). Round 2 re-ran the gates and
rechecked every round-1 resolution; all ten hold. Two majors reopen Phases 6
and 7; three minor documentation findings sit on Phase 4 and one each on
Phases 6 and 7; three notes are recorded. Each fix names the phase file that
carries it.

| ID    | Severity | Phase | Finding                                                                                                                                                                                                 | Fix |
| ----- | -------- | ----- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --- |
| R2-01 | major    | 6     | A recorded or default S3 target whose `region` is empty is dropped from the listing: `prepare` resolves the raw key region, the profile or ambient region wins, the resolved key dedupes the recorded group away, and `matches` cannot bind the record. The target vanishes with no `!` row and no hint, against invariant 7 and decision 54. | - [x] Resolve a recorded identity with `regionDefault(key.Region)`; `TestSpaceS3RecordWithoutRegion` passes |
| R2-02 | major    | 6, 7  | The picker's S3 create path calls `backend.Create` without `storage.ValidateNewSpaceName`, so a name outside the portable grammar (for example a dotted legacy name) reaches S3 and can be created. Hosted is protected only inside `sitelogin.CreateSpace`, which reports the raw grammar error. | - [x] `Space.pick` validates before any backend create; the picker test asserts the exact refusal and zero fake-admin create calls, and the valid create-and-select test passes (session 13) |
| R2-03 | minor    | 6     | The S3 ambiguity refusal prints `endpoint=... profile=...` and names only `--profile` or `--endpoint`. Two identities that differ only in region print identical rows, and the field that selects one, `--region`, is never named. | - [x] Candidate text includes region and ambiguity names the differing selector; README and `login.md` document it; `TestSpaceS3SetDefaultAmbiguous` passes |
| R2-04 | minor    | 4     | `spaceHelp` says an S3 group needs an S3 flag, an AWS variable, or a recorded target, and that a hosted-only machine reads no AWS configuration. The shared AWS files are also a signal (`s3Signals`/`awsFiles`), as `architecture/login.md` states. | - [x] Name the shared AWS files in help, retain the create-mode rule, and pin both in `TestSpaceUsageNamesTheModes` (session 14) |
| R2-05 | minor    | 7     | The picker title `Pick the default target for serve, pull and commit` promises that every target is used by those commands. This build uses only a hosted default; an S3 default is stored for the sibling worklog. | - [x] Use neutral title `Pick the default target` in the picker, README sketch, test assertion, and `architecture/tui.md` (session 13) |
| R2-06 | minor    | 4     | The README refusal example is `error: space new: x must be 3 to 63 ...`; the shipped text is `space new: storage: invalid new space name: "x" must be 3 to 63 ...`. | - [x] Align the README example and `architecture/login.md` with the shipped wrapper; keep the grammar error wrapped (session 14) |
| R2-07 | minor    | 4     | The README and `config.md` say an unreadable settings file refuses `space`; `PrepareSpace` skips the read in create mode, so `space new` succeeds. | - [x] Limit the refusal to listing and set, and state that create mode does not read the file (session 14) |
| R2-08 | note     | 5     | `Resolved.Profile` and `Identity.Profile` are documented as the profile that selected the credentials, but the value is `Config.Profile`. The app always passes `AWS_PROFILE`, so no user-visible defect. | - [x] Correct both comments to describe the configured shared-config selector and the `internal/app` `AWS_PROFILE` resolution (session 15) |
| R2-09 | note     | 8     | Round 1 wrote its findings into a standalone file and placed most of them in Phase 6 instead of the affected phases. Round 2 records in place; the standalone file stays as the round-1 history. | - [x] Closed by this review's in-place recording |
| R2-10 | note     | 6     | `SpaceHeader` and `Header()` are named after the `space` command but carry a store-identity group header, and for S3 they name a bucket identity rather than a space. | - [x] `architecture/login.md` states once that command-prefixed names describe the command surface, not only hosted spaces |

Review round 3 (2026-10-20, session 16). All ten round-2 findings were
rechecked against the current tree and remain resolved. The named tests below
passed in this review; R2-09's recording-placement fix is also now present in
this index, the affected phase files, and this journal.

| R2 finding | Re-check | Evidence |
| ---------- | -------- | -------- |
| R2-01 | Resolved | `Space.prepare` normalizes a recorded region before resolving it (`internal/app/space.go`); `TestSpaceS3RecordWithoutRegion` passes. |
| R2-02 | Resolved | `Space.pick` validates before `backend.Create` (`internal/app/space.go`); `TestSpacePickerRefusesAnInvalidS3Name` passes and asserts no create call. |
| R2-03 | Resolved | `ambiguousRefusal` prints region and names differing selectors (`internal/app/space.go`); `TestSpaceS3SetDefaultAmbiguous` passes. |
| R2-04 | Resolved | `spaceHelp` names the shared AWS files and the create-mode distinction (`cmd/login/login.go`); `TestSpaceUsageNamesTheModes` passes. |
| R2-05 | Resolved | The picker title is `Pick the default target` in code and docs; `TestSpacePickerCreatesAndSelects` asserts it. |
| R2-06 | Resolved | The README and `architecture/login.md` show the wrapped grammar refusal; `TestSpaceNewInvalidName` and `TestSpacePickerRefusesAnInvalidS3Name` pass. Round 3 separately found that the public plain CLI line is not pinned; see R3-02. |
| R2-07 | Resolved | `architecture/config.md` limits the settings read to listing/set and excludes create mode; `PrepareSpace` skips `spaceSettings` for create mode. Round 3 found no test for this promise; see R3-01. |
| R2-08 | Resolved | The `Resolved.Profile` and `Identity.Profile` comments describe the configured selector and `internal/app` environment resolution; `TestResolveReportsEffectiveValues` passes. |
| R2-09 | Resolved | Review findings now reside in this README and their affected phase files; the round-1 standalone record remains history. |
| R2-10 | Resolved | `architecture/login.md` states the command-prefix naming convention for `SpaceHeader`; this matches the cross-backend group header in `internal/app/space.go`. |

Review round 3 recorded three minor findings. R3-01 and R3-02 concern proof of
documented Phase 4 behavior, not failures observed in the implementation; both
were closed in session 17. Session 18 corrected the stale Phase 6 specification
for R3-03. Every review-round-3 finding is resolved. Shipped code and its proof
test match the README. Phases 4 and 6 stay Complete because no finding is a
blocker or major.

| ID    | Severity | Phase | Finding | Evidence | Fix |
| ----- | -------- | ----- | ------- | -------- | --- |
| R3-01 | minor | 4 | The docs promise that `space new` does not read or write `workspaces.json`, but no test proves this create-mode exception. | `architecture/config.md:75`; `PrepareSpace` bypasses `spaceSettings` for `spaceCreateMode` (`internal/app/space.go`); `TestSpaceNewDoesNotReadOrWriteSettings` passes and proves creation succeeds with malformed settings and leaves their bytes unchanged. | - [x] Add and pass the malformed-settings create test (session 17). |
| R3-02 | minor | 4 | The README's plain refusal example for `space new x` is exact, but no test pins the complete CLI stderr line. | README.md:411-412; `TestScenarioSpaceNewInvalidNameRefusal` passes and asserts nonzero exit, empty stdout, and exact CLI stderr. | - [x] Add and pass the black-box refusal scenario (session 17). |
| R3-03 | minor | 6 | The Phase 6 specification still says an S3 ambiguity refusal names only `--profile`/`--endpoint` and lists no region, although the shipped behavior includes `--region` when region differs. | Phase 6 specification and integration/error rows (`phase-6-space-s3.md:122-125`, `:230`, `:276`) conflict with `ambiguousRefusal` (`internal/app/space.go:980-1019`), `TestSpaceS3SetDefaultAmbiguous` (`internal/app/space_s3_test.go:690-697`), and the current README example. | - [x] Phase 6 now lists endpoint, region, and profile and requires differing selectors in `--profile`, `--region`, `--endpoint` order; the region-only expectation matches `TestSpaceS3SetDefaultAmbiguous` and the README example (session 18). |

### Validation round 6 (2026-10-20)

Prior findings were re-checked against their
cited edits before new findings were recorded.

| Prior IDs | Re-check | Evidence |
| --- | --- | --- |
| V1-01, V1-02, V1-03, V1-04, V1-05, V1-06, V1-07, V1-08, V1-09, V1-10, V1-11, V1-12, V1-13, V1-14, V1-15, V1-16 | Resolved | The phase file/contract, proof-test, rendering, documentation ownership, endpoint, and parameter edits remain present. V1-02 was closed by V2-09. |
| V2-01, V2-02, V2-03, V2-04, V2-05, V2-06, V2-07, V2-08, V2-09, V2-10, V2-11, V2-12, V2-13, V2-14, V2-15, V2-16, V2-17, V2-18 | Resolved | The separate dupl gate, phase-owned docs and tests, route/region/prefix ownership, links, examples, and output assertions remain in the cited phases and README. |
| V3-01, V3-02, V3-03, V3-04, V3-05, V3-06, V3-07, V3-08, V3-09, V3-10, V3-11, V3-12, V3-13, V3-14 | Resolved | Current region and selector docs, flags, examples, error tests, cloud e2e ownership, and helper boundaries match the cited closing edits. |
| V4-01, V4-02, V4-03, V4-04, V4-05, V4-06, V4-07, V4-08, V4-09, V4-10, V4-11, V4-12, V4-13, V4-14, V4-15, V4-16, V4-17, V4-18, V4-19 | Resolved | The current architecture text, CLI selectors and output, picker signal behavior, S3 containment, cloud gate ownership, and Phase 2 human-required handoff retain the cited edits. |
| V5-01, V5-03, V5-04, V5-05, V5-06, V5-07, V5-08, V5-09, V5-10, V5-11 | Resolved | Empty-region proof, hosted degradation, settings documentation, sorted examples, output behavior, error taxonomy, strict decoding, shared region ownership, grammar mirror, and `s3Admin` owner remain as cited. |
| V5-02 | Resolved | Phase 4 `Files` and `Acceptance criteria` now cite `TestSpaceRefusesAnExpiredLogin` and `TestSpaceExpiredLoginIsDegraded`; the tests cover the explicit expired-login refusal and degraded listing. |
| R2-01, R2-02, R2-03, R2-04, R2-05, R2-06, R2-07, R2-08, R2-09, R2-10 | Resolved | The region-normalization, creation-grammar, ambiguity, help, picker title, refusal, settings-read, profile comment, finding placement, and group-header edits remain; their named tests and docs are present. |
| R3-01, R3-02, R3-03 | Resolved | `TestSpaceNewDoesNotReadOrWriteSettings`, `TestScenarioSpaceNewInvalidNameRefusal`, and `TestSpaceS3SetDefaultAmbiguous` exist and passed the focused V6 runs; Phase 6 now names the normalized region and selector order. |

V5-02 was a minor evidence-ownership finding. Its class had already been run,
so it was not reopened as a major. V6 adds the following notes; all classes
were already exercised in V1–V5 or R2/R3, and no phase is reopened.

| ID | Severity | Class | Finding | Evidence | Fix |
| --- | --- | --- | --- | --- | --- |
| V6-01 | note | repository fact | Phase 2 `Files` lists three cloud end-to-end paths under `cloud/e2e/`, but the files are at the cloud repository root under `e2e/`. | `phase-2-cloud-create-route.md`, `Files`; the listed `cloud/e2e/tests/space-create.spec.ts`, `cloud/e2e/tests/cli-login.spec.ts`, and `cloud/e2e/playwright.config.ts` do not exist; `e2e/tests/space-create.spec.ts`, `e2e/tests/cli-login.spec.ts`, and `e2e/playwright.config.ts` do. | Correct the three paths in the Phase 2 `Files` table.<br>- [x] Corrected the paths; verified all three files in the local cloud checkout (session 20). |
| V6-02 | note | evidence ownership | Phase 5 adds S3 administration tests in a file omitted from its `Files` table. | `phase-5-s3-admin-and-profile.md`, `Files`; `internal/s3store/admin_test.go` exists and contains the profile, `Resolve`, `Admin`, and create tests named by Phase 5. | Add `internal/s3store/admin_test.go` to Phase 5 `Files`.<br>- [x] Added `internal/s3store/admin_test.go` (session 20). |
| V6-03 | note | evidence ownership | Phase 6 omits the S3 backend test file that contains most Phase 6 unit proofs. | `phase-6-space-s3.md`, `Files`; `internal/app/space_s3_test.go` exists and contains `TestSpaceS3RecordWithoutRegion`, the ambiguity test, and the S3 listing/create cases. | Add `internal/app/space_s3_test.go` to Phase 6 `Files`.<br>- [x] Added `internal/app/space_s3_test.go` (session 20). |
| V6-04 | note | evidence ownership | Phase 4 acceptance and error rows name two phase-local S3 refusal tests that no longer exist in the final tree. | `phase-4-space-new-hosted.md`, `Acceptance criteria` and `Error coverage`; `TestSpaceAutoS3SignalRefused` and `TestSpaceS3Unavailable` are absent after Phase 6 replaced those interim behaviors. | Replace the interim references with final proof tests or mark those rows as superseded by Phase 6.<br>- [x] Updated the acceptance and error rows to name the final Phase 6 behavior tests (session 20). |
| V6-05 | note | evidence ownership | Phase 1 uses `existing settings rows` instead of naming test evidence for the unchanged file mode, directory mode, size bound, exposed paths, and lock failures. | `phase-1-settings-v2.md`, `Acceptance criteria` and `Error coverage`; `internal/settings/settings_test.go` has `TestSaveModes`, `TestBoundRefusesOverSizeFile`, `TestLoadRefusesExposedFileAndDir`, and `TestSaveRefusesWhileLocked`. | Name the tests in the relevant Phase 1 rows.<br>- [x] Named the tests in the Acceptance and Error coverage rows (session 20). |
| V6-06 | note | evidence ownership | Phase 8's error-coverage rows have no test or repeatable command for each gate failure. | `phase-8-quality-gate.md`, `Error coverage`; rows describe remedies but name no command or test. | Name the relevant gate or repeatable check in each row.<br>- [x] Added the gate or command to every Error coverage row (session 20). |
| V6-07 | note | doc falsification | README invariant 6 says `space` reads a record only for directory display, but S3 records and defaults also add identities to the listing. | README `Invariants`; `Space.recordedS3Keys`, `Space.prepare`, and `s3Space.Default` read recorded S3 keys/defaults to prepare listing groups. The record still does not select the target of a create or default-setting operation. | State the narrower invariant: a record may add a listing group, but never selects the command's create or set-default target.<br>- [x] Narrowed invariant 6; retained `TestSpaceNeverTargetsARecord` and added S3 group proofs (session 20). |

## Session journal

### 2026-10-18: design interview and README (parent worklog)

Settled before writing: the `space` surface (`space` lists, `space <name>`
sets the default, `space new <name>` creates, the picker offers create);
backend-aware targets; hosted defaults stay in `credentials.json` while S3
defaults live in `settings`; settings version 2 with `kind`, `endpoint`,
`region`, `profile`, `name`, `prefix`, and `pathStyle`, plus a defaults list
keyed by the store key; the portable creation grammar; the `space` flag
surface with a backend-relative `--endpoint`; the AWS profile as the only
credential index; and lazy recording with no credential scanning.

Rejected: an environment-file feature; enumerating every `~/.aws` profile;
per-call backend switching in the MCP server; storing the access key id;
`encoding/json/v2` while it is experimental; advertising R2 before a live
probe.

### 2026-10-18: split into two worklogs

The single worklog bundled two outcomes: an additive admin surface and a
rewrite of how every command resolves and records a target, plus a breaking MCP
schema change. It was split by concern at design sign-off, before any phase
started. This worklog keeps the admin surface and the shared store model, in
eight phases; the sibling keeps the one-directory server and the backend-aware
bindings, in three. Two phases moved here unchanged apart from their numbers:
settings version 2 became Phase 1, the cloud route became Phase 2, the
`space` phases follow, and both quality gates replaced the single gate.

Boundary decisions recorded at the split:

- `--profile` and `s3store.Config.Profile` belong here, because the S3 `space`
  backend cannot exist without them. The record layer over the profile and over
  the effective values, the default lookup, `workspace.Identity.Profile`, and the
  probe proof version 2 belong to the sibling.
- The settings file version, its encoder, its decoder, and the store key are
  here. Writing the effective endpoint into a record is the sibling's.
- `space` never writes a record and never takes its target from one.

Status: README signed off; phases are written.

### 2026-10-18: validation round 1

The readiness checklist was run against the written phases. Item 2 failed,
because only Phase 1 named files; items 3, 5, and 7 needed sharpening; items 1,
4, and 6 passed. Six majors, eight minors, and two notes were filed and closed
in place. No contract was redesigned, so the README stays signed off and every
phase stays Not Started. The finding-to-edit map is the feedback index above.

The class-level corrections, rather than the cited lines: each phase now names
the files that hold its tests; the README invariants point at a test in the
phase that owns it; the picker owns the terminal harness it needs; the helpers
shared with `login` are reconciled in the phase that changes them; every phase
that changes a documented behavior updates that doc; and the settings field
rule no longer contradicts the sibling boundary.

### 2026-10-18: validation round 2

The readiness checklist was re-run and each round-1 finding was re-checked
against the edit it cites. V1-01 and V1-03 to V1-16 are resolved. V1-02 stayed
open when re-checked, because invariant 3 still named Phase 2 without a Phase 2
test, and V2-09 closes it. Six majors, eight minors, and four notes were filed
and closed in place: the public gate no longer claims dupl is in `make qa`;
Phase 4 owns the documented output change and the assertions it rewrites;
Phase 2 has one creation-grammar owner and names `cloud/site/accounts.ts`;
Phase 5 owns the region rule; Phase 6 validates the prefix before it stores it;
and the Files tables name the remaining edited files. No contract was
redesigned, so the README stays signed off and every phase stays Not Started.
The finding-to-edit map is the feedback index above.

### 2026-10-18: validation round 3

The readiness checklist was re-run and each round-2 finding was re-checked. All
are resolved and none regressed. The round was then re-scored against one rule:
a major is a contract violation that survives a competent phase worker and a
green gate. Twelve of the candidates fell to minor or note, because the worker
or the suite settles them. Two remained: the unowned doc sentences that Phase 5
falsifies (V3-02), and hints that print a flag order the command refuses
(V3-03). The checklist gains item 8, so a phase that changes a shared value must
name the doc sentences it falsifies and prove them gone. No contract was
redesigned; every phase stays Not Started.

### 2026-10-19: validation round 4

The readiness checklist was re-run and each round-3 finding was re-checked.
V3-02, V3-07, V3-09 and V3-12 were still open, and four round-4 findings close
them. The round then re-scored every finding against one rule: a major is a
contract violation that survives a competent phase worker and a green gate.
Four remained: the `space` transcript in `running.md` and the S3 `space` surface
in `login.md`/`cli.md` had no doc owner; the hosted header and refusal printed
the site while `--endpoint` matches the storage endpoint; and the picker's
`interruptible` scope would swallow SIGINT. The rest are minors and notes. The
class-level corrections: a header prints the value its selector accepts; only
the prompt read takes a context; and every phase that changes `space` owns its
sentences in `login.md`, `cli.md` and `running.md`. No contract was redesigned
and every phase stays Not Started. The finding-to-edit map is the feedback index
above.

### 2026-10-19: validation round 5

The readiness checklist was re-run and each round-4 finding was re-checked; all
are resolved. Nine minors and two notes were filed and closed in place. The
round ran the evidence sweep (every named test against an owning phase and
file) and the shared-value sweep (every changed default, flag, marker, and
printed line against `architecture/`), and re-checked each `rg` check that
proves a sentence gone: `a space and a prefix only`, `owned by`, `S3  endpoint=`,
`s3:CreateBucket`, `--profile`, and `region is required` all still need their
phase. Every class had already run, so no finding rose above minor. No contract
was redesigned and every phase stays Not Started.

### 2026-10-19: phase 1 implemented

Worker: phase-1 agent, 2026-10-19, session 1. Phase 1 shipped: settings
version 2 with a backend-aware `Target`, `StoreKey`, and a store-keyed
`defaults` list, and a version-1 read path that never rewrites. The details,
decisions, and command results are in
[phase-1-settings-v2.md](./phase-1-settings-v2.md). The status board and the
phase file were updated in the same change, and no other phase started.

Gates: `go build ./...`, `go vet ./...`, `gofumpt`, `go fix -diff ./...`,
`staticcheck`, and `go test -count=1 ./internal/settings/...
./internal/app/... ./internal/integrationtest/...` were clean; `make test`
(race, `-count=3`, 70 % floor) passed.

### 2026-10-19: implementation environment confirmed

The worklog declares `Human required` nowhere: the cloud checkout at
`/home/imago/Projects/private/slivingdoc-cloud` is present and writable, its
Node dependencies and the Playwright browsers are installed, the Docker daemon
serves the pinned SeaweedFS container, and `go build ./...` with `go vet ./...`
is clean on the untouched tree. Phase 2 and Phase 8 state that setup instead of
stopping for a person. The earlier findings that cite the removed section
stay as history.

### 2026-10-19: phase 2 implemented

Worker: phase-2 agent, 2026-10-19, session 1. Phase 2 shipped in the cloud
checkout: `validNewSpaceName` as the one creation grammar, `CliLogins.create`,
and `POST /cli/v1/spaces` under `cliSpaces`, with the account form moved to the
same grammar. The details, decisions, and command results are in
[phase-2-cloud-create-route.md](./phase-2-cloud-create-route.md). The status
board and the phase file were updated in the same change, and no other phase
started.

The checkout already carried an uncommitted implementation of the phase when
the session began. This session checked it against the phase file row by row,
changed the create body reader to return `null` instead of throwing, and moved
the end-to-end create flow into its own production-excluded
`e2e/tests/space-create.spec.ts`, because the repository's lint rule
`T1-lint-rejects-skipped-tests` refuses a conditional `test.skip` and the
admin-page specs are excluded the same way.

Gates: `make qa` in the cloud checkout exits zero (lint, build, fixture, 33 site
test files, 26 cloud test files, links, dupl, Workers' typecheck and tests at
95.2 % statements), `npx prettier --check .`, `npx knip`, and
`cd e2e && npm run typecheck` are clean, and both local-stack Playwright runs
pass: `tests/space-create.spec.ts` (1 test, 21.0 s) and
`tests/cli-login.spec.ts` (3 tests, 20.6 s). No public-repo code changed;
`go build ./...` and `go vet ./...` are clean on the public tree.


### 2026-10-19: phase 3 implemented

Worker: phase-3 agent, 2026-10-19, session 2. Phase 3 shipped in the public
repository: `storage.ValidateNewSpaceName`, `sitelogin.Client.CreateSpace`, the
any-2xx answer rule in `Client.call`, and the reference site's create route with
`Issue.Account`, `RefuseCreate` and duplicate-member refusal. The details,
decisions, and command results are in
[phase-3-sitelogin-create-space.md](./phase-3-sitelogin-create-space.md). The
status board and the phase file were updated in the same change, and no other
phase started.

Gates: `go test -race -count=3 -timeout=30s ./internal/sitelogin/...
./internal/storage/...` passes, and `make qa` (gofumpt, vet, staticcheck,
`go fix`, the full race/count=3 suite, npm) exits zero with coverage 89.0 %
against the 70 % floor. `architecture/login.md` and `architecture/storage.md`
document the new wire contract and the portable creation grammar.

### 2026-10-19: phase 4 implemented

Worker: phase-4 agent, 2026-10-19, session 3 (continued from the partial tree
an earlier session left). Phase 4 shipped the hosted `space` backend:
`PrepareSpace` and `spaceBackend` in `internal/app/space.go`, the three
positional forms `space`, `space <name>` and `space new <name>`, hosted groups
with directory annotations and `*`/`!`/`*!` markers, a degraded listing that
exits zero with a literal `! ` hint, orphan and missing-default rows, create
through `sitelogin.CreateSpace` without changing the default, default storage
under the credentials lock, and the phase-local S3 refusals. The space renderer
moved to `internal/app/present.go`; the login flow's `spacesTable`, `pickSpace`
and `describeSpace` are unchanged. Tests are in `internal/app/space_test.go` and
`internal/integrationtest/scenario_space_hosted_test.go`; the existing plain
`space` assertions were rewritten to the new form. The details, decisions, and
review findings are in
[phase-4-space-new-hosted.md](./phase-4-space-new-hosted.md). The status board
and the phase file were updated in the same change, and no other phase started.

Gates: `go build ./...`, `go vet ./...`, `gofumpt -l .`, `staticcheck ./...`,
`go fix -diff ./...`, and `go test ./internal/app/ ./internal/cli/
./internal/integrationtest/ -count=1` are clean. `make qa` exits zero with
coverage 89.1 % against the 70 % floor and the npm tests passing.
`go run github.com/mibk/dupl@v1.0.0 -t 80 .` reports three clone groups, all
triaged as acceptable: one declarative flag-registration pair and two
pre-existing test tables. `rg -n 'owned by' architecture/running.md` now hits
the unchanged login transcript only. `architecture/login.md`,
`architecture/cli.md`, `architecture/running.md`, `architecture/config.md`,
`architecture/tui.md` and `architecture/decisions.md` were corrected in the same
change.

### 2026-10-19: phase 5 implemented

Worker: phase-5 agent, 2026-10-19, session 4. Phase 5 shipped the S3
administration surface: `s3store.Config.Profile`, the shared AWS configuration
loader, `Resolve`/`Resolved`/`Store.Resolved`, and `Admin` with `ListBuckets`,
`CreateBucket`, and `Identity`, including the create-configuration rule and
the `ErrBucketExists`/`ErrBucketOwned` sentinels. `app.Flags` gained
`--profile`, the shared region now has an empty default and the order
`--region`, `AWS_REGION`, `AWS_DEFAULT_REGION`, the profile's region, then
`us-east-1`, and `realStoreFactory` passes the profile. The details, decisions,
and review findings are in
[phase-5-s3-admin-and-profile.md](./phase-5-s3-admin-and-profile.md). The status
board and the phase file were updated in the same change, and no other phase
started.

The worktree already carried the phase-5 code and no-network tests when the
session began. This session checked each acceptance and error row against it,
added the named region acceptance tests, and completed the architecture docs.

Gates: `go test ./internal/s3store/ ./internal/app/ -count=1` passes before and
after the audit; `make qa` exits zero with coverage 89.1 % against the 70 %
floor (gofumpt, vet, staticcheck, `go fix`, the race suite at `-count=3`, and
the npm tests). The removed-doc and AWS-import checks print nothing, and
`--profile` appears in `architecture/config.md`, `architecture/login.md`, and
`architecture/running.md`.

### 2026-10-19: phase 6 partial implementation and handover

Worker: phase-6 agent, 2026-10-19, session 5. The session reached the context
limit with Phase 6 partly implemented and the tree uncompilable. The phase file
carries the complete handover: what is done (the `s3store` `DefaultRegion` and
`ErrProfileNotFound` additions, verified by
`go test ./internal/s3store/ -count=1`; the `newS3Admin` seam; most of the
`space.go` selection, identity, create, and default logic), the exact remaining
`go build ./internal/app/` errors, and the ordered list of remaining work
(the `s3Space` backend, the `listing` error return and orphan insertion, the
`spaceCandidate` header, `present.go` rendering, the help text, the unit and
integration tests, and the five architecture docs). The material decisions are
recorded in the phase file too. No gate was run on the app tree because it does
not compile; the next session starts by finishing the compile errors, then
writes the tests first.

### 2026-10-19: phase 6 implemented

Worker: phase-6 agent, 2026-10-19, session 6. The working tree already carried
the handover items when the session began, and `go build ./...`, `go vet ./...`,
and the app and S3 tests were clean. This session checked every acceptance and
error row against the tree, found no missing behavior, strengthened
`TestSpaceS3DegradedList` with a hosted group, and completed the
documentation. `architecture/login.md` lost the phase-4 paragraph that said
`--storage s3` was refused and that an S3 signal produced a phase-local
refusal; it now describes the S3 listing, the four create refusals, the
store-keyed default with prefix and path style, the display-only key id, and
the auto rule. `architecture/s3store.md` gained the corrected `DefaultRegion`
and `ErrProfileNotFound` symbols and the key-id rule; `architecture/security.md`
states that the access key id is display only; `architecture/errors.md` states
that a `space` refusal is plain CLI text outside the taxonomy; `architecture/cli.md`
names both backends in the `Space.Run` flow; and `architecture/testing.md` names
the new S3 scenario. The details and review findings are in
[phase-6-space-s3.md](./phase-6-space-s3.md). The status board and the phase file
were updated in the same change, and no other phase started.

Gates: `go test ./internal/app/ ./internal/s3store/ ./internal/integrationtest/ -count=1`
passed before and after the doc review; `go test -race -count=3 -timeout=30s
./internal/app/ ./internal/s3store/ ./internal/integrationtest/` passed;
`gofumpt -l .` and `go vet ./...` printed nothing; `make qa` exited zero with
coverage 89.2 % against the 70 % floor and all 35 npm tests passing; and
`go run github.com/mibk/dupl@v1.0.0 -t 80 .` reported three clone groups, all
triaged as acceptable (the declarative flag registration pair and two
pre-existing test tables).

### 2026-10-20: phase 7 implemented

Worker: phase-7 agent, 2026-10-20, session 7. The working tree already carried
the interactive picker when the session began: `Space.list` opened the table on
a terminal, `Space.pick` mapped a `spaceChoice` to `SetDefault` or `Create`,
`promptName` read the create name under `ProcessOptions.interruptible`, and
`present.go` held the documented columns and create rows. This session audited
every acceptance and error row against the tree, added the named
`TestSpacePickerSkip` and a degraded-backend create-row subtest, and fixed the
one real defect the first `make qa` found: the PTY harness imported `syscall`
to read a signal exit, which `TestNoGitExecutableOrGit2goImport` refuses
process-wide. The harness now reaps its child with `unix.Wait4` in
`ttyProcess`, so `TestScenarioSpacePickerInterruptAtTable` still gets exit 130
without the forbidden import.

`architecture/testing.md` now names
`internal/integrationtest/scenario_space_tty_linux_test.go` and
`runCLIOnTypedTTY` as the picker's PTY helper, with the `openPTY` capability
skip, and lists the helper in the terminal seam row. `architecture/cli.md` and
`architecture/tui.md` now state that the synthetic no-login hosted group is
not a configured backend: the picker leaves its rows out, while the plain
listing keeps them and prints their `! ` hint before the table. Decision 53 in
`architecture/decisions.md` records the picker behavior, the create-row
default, the prompt's signal rule, and that omission. The status board and the
phase file were updated in the same change, and no other phase started.

Gates: `go test ./internal/app/ ./internal/integrationtest/ -count=1` passed
before the documentation edits; the picker scenarios passed under
`go test -race -count=1 -timeout=120s -run
'TestSpacePicker|TestSpacePlainUnchanged|TestTerminalHarnessRunCLIOnTypedTTY|TestScenarioSpacePicker'`;
exactly `go test -race -count=3 -timeout=30s ./internal/app/
./internal/integrationtest/` passed (13.5 s and 21.0 s); `make qa` exited zero
with gofumpt, `go vet`, staticcheck, and `go fix -diff` silent, coverage
89.3 % against the 70 % floor, and all 35 npm tests passing; and
`go run github.com/mibk/dupl@v1.0.0 -t 80 .` reported the same three clone
groups as the phase-6 gate, all acceptable under the duplication policy. Two
full `make qa` runs passed; one earlier full run hit the suite's 30 s test
binary timeout during the parallel run and a rerun passed, with no timeout,
count, skip, or race setting changed. The details and review findings are in
[phase-7-space-picker.md](./phase-7-space-picker.md).

### 2026-10-20: phase 8 implemented and the worklog closed

Worker: phase-8 agent, 2026-10-20, session 8. The public gate passed before
and after the corrections: `make qa` exited zero with gofumpt, `go vet`,
staticcheck and `go fix -diff` silent, the race suite at `-count=3` green,
coverage 89.3 % against the 70 % floor, and 35 npm tests passing with none
skipped. Dupl reported the same three groups both times; all three are the
documented acceptable patterns (the declarative flag registries and two
test-table pairs). The cloud gate `make -C "$CLOUD_REPO" qa` exited zero, with
the gateway's 26 files and 670 tests plus the site's 33 files and 593 tests,
links, and its 2 known jscpd clones. The local e2e run of `cli-login.spec.ts`
and `space-create.spec.ts` passed 4 tests in 40.2 s against
`slivingdoc.localhost`, reaching no deployed site or AWS endpoint.

The architecture review found the docs true in substance and six
documentation defects, all fixed in the same change: the phase-4 picker
wording survived in `spaceHelp` and `running.md`; `login.md` carried duplicate
`Key files` rows for `present.go` and `space.go`; `config.md` and
`hosted-mode.md` recorded the sibling worklog as a future writer, which is a
plan in behavior prose; and the worklog README's flow and picker sketch
described the wrong terminal condition and table shape. No behavior, test,
flag, error category, or protocol contract changed. Decision 52 and decision
53 already carry this worklog's architecture decisions with their dates, and
decisions 46 and 49 carry the hosted create and settings-version-2 changes;
this phase made no new decision. The status board, the phase file, and the
readiness checklist are complete; the worklog is closed.

### 2026-10-20: post-close holistic review (session 9)

Worker: holistic-review agent, 2026-10-20, session 9. All eight phases were
complete, so this session reviewed the implemented work. `make qa` passed
before the review (`exit 0`, coverage 89.3 %, 35 npm tests), and
`go build ./...` and `go vet ./...` were clean. The review found ten open
items, one of them a behavior defect: `space` creates phantom S3 groups from
partial records and its wildcard directory match prints one directory under
several identities. The full findings, evidence, and the recommended fix are
in [review-2026-10-20.md](./review-2026-10-20.md). No production code
changed in this session; the only temporary test edit was reverted. The
next session starts with the S3 grouping fix and records the chosen matching
semantics in `architecture/decisions.md`.

### 2026-10-20: post-close review remediation (session 10)

Worker: review-remediation agent, 2026-10-20, session 10. All eight phases were
complete and the session-9 review was open, so this session resolved its ten
findings instead of opening a new phase. The behavior change is exact S3
identity matching: an empty endpoint is the provider default and an empty
profile the ambient chain, an empty region normalizes to `us-east-1`, and
recorded store keys are deduped after resolution. Decision 54 records it. The
picker now refuses an unreachable `!` row exactly as `space <name>` does. In
`auto`, an environment-selected S3 identity that cannot resolve degrades and
keeps the other groups, while `--storage s3` or a flag that names the identity
refuses. The backend `Default` lookup lost its error, path style is recorded
for any custom endpoint, and `spaceHelp`, `architecture/config.md`,
`architecture/login.md`, `architecture/running.md`, `architecture/cli.md`,
`architecture/tui.md`, decision 49, the phase-4 and phase-6 snippets, and the
`present.go` preamble now state the shipped behavior. The full disposition is
in [review-2026-10-20.md](./review-2026-10-20.md); the phase-6 file records the
revised semantics and the remediation list.

Gates: `go test ./internal/app ./internal/settings ./internal/s3store
-count=1` passed before the change. After it, `go test
./internal/app -count=1` passed, then `go test ./internal/cli
./internal/integrationtest ./internal/settings ./internal/s3store -count=1`
passed. `make qa` exited zero twice, the final run with gofumpt, `go vet`,
staticcheck and `go fix -diff` silent, the race suite at `-count=3` green,
coverage 89.4 % against the 70 % floor, and 35 npm tests passing with none
failed. `go run github.com/mibk/dupl@v1.0.0 -t 80 .` reported the same three
clone groups as the phase-8 gate, all acceptable under the duplication policy.

### 2026-10-20: post-close review round 2 (session 11)

Worker: review agent, 2026-10-20, session 11. All phases were marked complete,
so this session reviewed the implemented work. `make qa` exited zero before the
review: gofumpt, `go vet`, staticcheck and `go fix -diff` silent, the race
suite at `-count=3` green, coverage 89.4 % against the 70 % floor, and the 35
npm tests passing with none skipped. The only tree changes were temporary proof
tests under `internal/app`, removed before this review was written.

Every round-1 resolution was re-checked against the tree and holds: exact S3
matching for endpoint and profile, reachable-only defaults in the plain and
picker paths, the environment-selected identity degradation, the removed
`Default` error, the corrected help and docs, and the path-style rule at the
backend boundary. Two new behavior defects and eight minor or note items were
filed; the Feedback index carries the IDs and the phase files carry the work.

Both majors are silent failures of a promise the README makes:

1. **R2-01.** A recorded or default S3 target whose `region` is empty is
dropped from the listing, because `prepare` resolves the raw key region and the
profile or ambient region then wins: the recorded key dedupes against the
current identity and `matches` cannot bind the record. A demonstration with a
record for `/a` and `profile dev` resolved to `eu-north-1` printed the header
and `  analytics` with no directory and no hint.
2. **R2-02.** The picker's S3 create path sends a name to S3 without
`storage.ValidateNewSpaceName`. A demonstration with the fake administration
created `Bad_Name`, so a dotted legacy name would be created by S3 even though
the portable grammar forbids it.

The minors are the S3 ambiguity refusal omitting the region (R2-03), the
`spaceHelp` S3-signal sentence (R2-04), the picker title over-promising S3
defaults (R2-05), the invalid-name refusal example (R2-06), and the unreadable
settings sentence (R2-07). The notes are `Resolved.Profile` (R2-08), the
round-1 recording placement (R2-09), and the `SpaceHeader` name (R2-10).

Verdict: the work is not ready. The gates are green, but correctness is not;
Phases 6 and 7 are reopened, and Phase 4 carries three open minor doc fixes.
Commands re-run:

| Command | Result |
| --- | --- |
| `make qa` | exit 0; coverage 89.4 %; 35 npm tests, none failed or skipped |
| `go test ./internal/app -run '^TestTemp…$' -count=1 -v` | the three proofs above; the temporary files were deleted |

### 2026-10-20: Phase 6 remediation (session 12)

Completed R2-01, R2-02, R2-03 and R2-10 remediation and verified Phase 6.

The focused remediation check passed; all three tests passed:

```bash
go test ./internal/app -run 'TestSpaceS3RecordWithoutRegion|TestSpacePickerRefusesAnInvalidS3Name|TestSpaceS3SetDefaultAmbiguous' -count=1 -v
```

`make qa` exited 0. Formatting, vet, staticcheck, `go fix -diff`, build,
and the full race test suite passed. Coverage was 89.3% against the 70% floor.
All 35 npm tests passed, with none skipped.

The dupl command reported three clone groups, all acceptable under the
duplication policy:

```bash
go run github.com/mibk/dupl@v1.0.0 -t 80 .
```

One group is declarative flag registration in `internal/app/login.go` and
`internal/app/space.go`; the other two are pre-existing test tables.

The prior `internal/integrationtest` timeout did not recur. Phase 6 is
Complete; Phase 7 remains the only reopened phase.

### 2026-10-20: Phase 7 remediation (session 13)

Verified the shared picker validation for R2-02 and closed R2-05 with the
neutral picker title in the implementation, README sketch, test assertion, and
`architecture/tui.md`. The requested focused app tests passed:

```bash
go test ./internal/app -run 'TestSpacePickerRefusesAnInvalidS3Name|TestSpacePickerCreatesAndSelects|TestSpacePickerSkip|TestSpacePickerPromptSkips|TestSpacePickerRefusesUnreachable|TestScenarioSpacePickerInterruptAtTable|TestSpacePlainUnchanged' -count=1 -v
```

The interrupt test is in `internal/integrationtest`, so it was also run there
directly and passed:

```bash
go test ./internal/integrationtest -run '^TestScenarioSpacePickerInterruptAtTable$' -count=1 -v
```

The coordinator ran `make qa` after the phase edits on this identical tree. It
exited 0 with 89.4% coverage against the 70% floor. All 35 npm tests passed,
none were skipped, and all lint steps were silent. This session did not rerun
`make qa`.

The dupl check passed triage with three existing clone groups:

```bash
go run github.com/mibk/dupl@v1.0.0 -t 80 .
```

Declarative flag registrations in `internal/app/login.go` and
`internal/app/space.go` register separate command-specific flags. One test
fixture table is repeated at the public integration and notebook boundaries;
those tests prove separate contracts. The other test clone is two rows in the
Git merge table, with distinct conflict shapes and expected results. These are
acceptable test tables, not duplicated test bodies. R2-02 and R2-05 are
resolved; Phase 7 is Complete.

### 2026-10-20: Phase 4 remediation (session 14)

Closed R2-04, R2-06 and R2-07. The focused CLI/app tests passed. `make qa`
passed with 89.3% coverage and all 35 npm tests; dupl found three acceptable
clone groups and no actionable duplication. The first focused run exposed a
line-wrap-sensitive help assertion; the test now normalizes whitespace before
checking the corrected help sentence.

### 2026-10-20: Phase 5 note remediation (session 15)

Closed R2-08 by correcting the `Resolved.Profile` and `Identity.Profile`
comments. They now describe the shared-config selector named by the
configuration, empty when the ambient chain applies, and note that
`internal/app` fills it from `--profile` or `AWS_PROFILE`. The targeted package test passed.
`make qa` passed with 89.3% coverage and all 35 npm tests. Dupl reported three
acceptable clone groups: test tables for separate contracts or conflict shapes,
and declarative command-specific flag registrations.

### 2026-10-20: Phase 5 note remediation (session 15)

Closed R2-08 by documenting `Resolved.Profile` and `Identity.Profile` as the
shared-config selector named by the configuration, empty when the ambient
chain applies; the comments also state that `internal/app` fills the value
from `AWS_PROFILE`. The architecture S3 identity text and the S3 identity
surface row remain accurate.

### 2026-10-20: holistic review round 3 (session 16)

Worker: holistic review. All eight phases remain Complete. The round rechecked
all ten R2 findings; each remains resolved. The review found two minor Phase 4
test-evidence gaps, R3-01 and R3-02, recorded above and in
[phase-4-space-new-hosted.md](./phase-4-space-new-hosted.md), and one stale
Phase 6 specification, R3-03, recorded above and in
[phase-6-space-s3.md](./phase-6-space-s3.md). The Phase 4 code paths implement
their documented behaviors, but tests do not prove the settings-file exception
or the complete public invalid-name refusal. Phase 6 code and its test match the
README; its own spec has stale selector wording. No phase is reopened because
none of these findings is a blocker or major. At review time, the worklog was
not ready for final closure until the specification was corrected and both
tests were added. Session 17 closed the Phase 4 test gaps; only R3-03, the
Phase 6 specification edit, now remains.

| Command | Result |
| --- | --- |
| `make qa` | exit 0. Formatting, vet, staticcheck, `go fix -diff`, and the race suite at count 3 passed. Coverage was 89.4% against the 70% floor. npm passed 35 tests; none failed or were skipped. No timeout occurred, so CPU pressure was not checked and no retry was needed. |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | exit 0; three clone groups. All are acceptable: separate command flag registrations, public-integration/notebook test setup, and rows of one Git merge test table. No actionable duplication. |
| `go test -count=1 -run=TestSpace ./internal/app` | pass; includes the R2-01, R2-02, R2-03, R2-05, R2-06 and R2-07 named app tests. |
| `go test -count=1 -run=TestSpaceUsageNamesTheModes ./internal/cli` | pass; R2-04 help and assertion. |
| `go test -count=1 -run=TestResolveReportsEffectiveValues ./internal/s3store` | pass; R2-08 profile resolution proof. |
| `go test -count=1 -run=TestScenarioSpace ./internal/integrationtest` | pass; hosted and S3 plain-output scenarios. |
| `go test -count=1 ./internal/cli -run '^TestSpaceUsageNamesTheModes$'`; `go test -count=1 ./internal/s3store -run '^TestResolveReportsEffectiveValues$'`; `go test -count=1 ./internal/integrationtest -run '^TestScenarioSpace(Hosted|S3)$'` | Returned `[no tests to run]`; not treated as evidence. The corrected named filters above and `-run=TestSpace` passed. |
| `rg -n 'a space and a prefix only|always explicit|setting are ignored|region is required|Pick the default target for serve, pull and commit|S3 group needs an S3 flag' architecture` | no matches; the stale sentences checked by earlier rounds remain absent. |
| `rg -n 'Create.*Settings|Settings.*Create|workspaces.json' internal/app/space_test.go internal/integrationtest/scenario_space_hosted_test.go` | no matches; no create-mode settings test. |
| `rg -n 'space new.*invalid|invalid.*space new|invalid new space' internal/integrationtest` | no matches; no black-box invalid-name refusal scenario. |
| Check of Phase 6's ambiguity prose against implementation/test | `phase-6-space-s3.md` still omits region in its setting, integration, and error rows; `ambiguousRefusal` and `TestSpaceS3SetDefaultAmbiguous` include `--region`. R3-03. |

The readiness checklist and evidence sweep were run against the tree. Items 1,
2, and 5–7 pass. Item 3 has the two gaps above. Item 8 finds R3-03: the Phase 6
specification still describes the old ambiguity text. Item 4 remains supported
by the environment evidence in session 8; this review did not recheck the
separate cloud checkout. Invariants 8 and 9 hold in code and their proofs pass:
`TestSpaceS3RecordWithoutRegion` and `TestSpacePickerRefusesAnInvalidS3Name`.
The plain hosted and S3 examples match `TestScenarioSpaceHosted`,
`TestScenarioSpaceS3`, and the row/picker tests; R3-02 is limited to the
specific plain refusal example. The architecture evidence sweep found each
named document consistent with its current code surface:

| Document | Verified surface |
| -------- | ---------------- |
| `architecture/login.md` | Hosted/S3 listing and selectors; R2-03 ambiguity selector; R2-06 refusal text; R2-10 naming convention. |
| `architecture/cli.md` | `space` argument forms, flag order, backend flow, and picker output streams. |
| `architecture/config.md` | Settings format, create-mode exception (test gap R3-01), and region/profile precedence. |
| `architecture/hosted-mode.md` | Version-2 hosted record fields and the selected login's endpoint. |
| `architecture/running.md` | Hosted transcript, S3 surface, shared flags, and region resolution. |
| `architecture/s3store.md` | Shared AWS loader, profile comments, resolved values, admin calls and error mapping. |
| `architecture/storage.md` | Portable creation grammar; notebook storage protocol remains unchanged. |
| `architecture/tui.md` | Picker title, columns, create rows, prompt and signal behavior. |
| `architecture/testing.md` | Hosted/S3 scenarios and the typed PTY picker harness. |
| `architecture/errors.md` | `space` refusals remain plain CLI text outside the MCP taxonomy. |
| `architecture/security.md` | Profile is a selector; access-key display and secret handling match the code. |
| `architecture/decisions.md` | Decisions 49 and 52–54 match the settings, S3 identity and picker behavior. |
| `architecture/overview.md` | Package map includes the space surface and backend-aware settings registry. |

The status board matches all eight phase statuses; Phases 4 and 6 stay Complete
under the minor-finding rule. The cloud gate was not rerun in this
repository-scoped review; Phase 8's recorded cloud-gate result remains the
available evidence.

### 2026-10-20: Phase 4 review-round-3 remediation (session 17)

Closed R3-01 and R3-02 with the two tests and full quality gate recorded in
[phase-4-space-new-hosted.md](./phase-4-space-new-hosted.md). Phase 4 stays
Complete. Only R3-03 remains, a Phase 6 specification edit.

### 2026-10-20: Phase 6 review-round-3 remediation (session 18)

Closed R3-03 by aligning the Phase 6 specification, integration contract, and
error coverage with `ambiguousRefusal` and the region-only test/example. Every
round-3 finding is resolved; Phase 6 remains Complete. `rg` checks and
`git diff --check` passed. No source, test, or architecture file changed, so
the `make qa` result recorded in session 17 remains valid for the identical
code tree: 89.3% coverage and 35 npm tests.

### 2026-10-20: close-out verification (coordinator)

After the session-18 worklog edits, the coordinator ran the gates on the final
tree. `make qa` exited zero: gofumpt, `go vet`, staticcheck and `go fix -diff`
silent, the race suite at `-count=3` green, coverage 89.3 % against the 70 %
floor, and all 35 npm tests passing with none skipped. `git diff --check`
reported nothing. No open checkbox remains in the Feedback index, all eight
phases are Complete, and every round-2 and round-3 finding is resolved.
Earlier gate runs in sessions 13 to 17 sometimes hit the fixed 30 s
per-package timeout during host CPU-contention bursts; every retry under low
`/proc/pressure/cpu` passed, and no timeout setting, count, or race flag was
changed.

### 2026-10-20: final validation V6 (session 19)

All eight phase files have the seven required sections. Their headers match the
Complete status board, and local Markdown links resolve. The shared diagram,
phase dependencies, scope, and conventions match the current code. The
Parameters and owners table was checked. Invariants 8 and 9 hold; their proof
tests passed. Checklist item 2 has notes V6-01 through V6-03, item 3 has V6-07,
and the named-test evidence checks found V5-02 and V6-04 through V6-06. Items
1, 4, 5, 6, and 7 pass. Item 8's architecture sweep found no stale values; the
README search matched invariant 3's unrelated creation rule and historical
feedback/journal text, not current output examples. In
`architecture/running.md`, `owned by` remains only in the unchanged login
transcript.

The prior-finding table above records every ID. All V1–V4 findings, V5-01 and
V5-03 through V5-11, and R2-01 through R2-10 and R3-01 through R3-03 were
resolved at this checkpoint. Session 19 had not yet reconciled the Phase 4
test citation for V5-02. V6 filed seven notes and closed at the filing
verdict **Conditionally ready**. Session 20 resolved V5-02 and all seven V6
findings, so no minor or note remains and the verdict is **Ready**; no phase
reopened.

| Command or check | Result |
| --- | --- |
| `make qa` | Exit 0. Gofumpt, `go vet`, staticcheck, and `go fix -diff` passed. Race tests passed at count 3. Coverage was 89.4% against the 70% floor. All 35 npm tests passed. No timeout occurred; no CPU-pressure retry was needed. |
| `go run github.com/mibk/dupl@v1.0.0 -t 80 .` | Exit 0. Three groups: separate app flag registrations, public integration/notebook test setup, and rows in one Git merge table. All are acceptable under the repository duplication policy. |
| `go test ./internal/app -run '^(TestSpaceS3RecordWithoutRegion|TestSpacePickerRefusesAnInvalidS3Name|TestSpaceNewDoesNotReadOrWriteSettings|TestScenarioSpaceNewInvalidNameRefusal)$' -count=1` | Pass for matching app tests. The scenario name is not in this package; it was run separately in `internal/integrationtest` below. |
| `go test ./internal/app -run '^(TestSpaceS3RecordWithoutRegion|TestSpacePickerRefusesAnInvalidS3Name|TestSpaceS3SetDefaultAmbiguous|TestSpaceNewDoesNotReadOrWriteSettings)$' -count=1` | Pass. Proofs for invariants 8 and 9 and R2-01, R2-02, R2-03, and R3-01. |
| `go test ./internal/integrationtest -run '^TestScenarioSpaceNewInvalidNameRefusal$' -count=1` | Pass; R3-02 exact CLI refusal proof. |
| `rg -n 'a space and a prefix only|always explicit|setting are ignored|region is required|Pick the default target for serve, pull and commit|S3 group needs an S3 flag|at most one positional argument' architecture` | No matches. The old architecture sentences and values are absent. |
| `rg -n 'owned by' architecture/running.md` | One match in the login transcript only; the stale `space` transcript is gone. |
| Same old-value expression under `worklogs/26-10-18-space-admin-surface/README.md` | Matches occur only in historical findings, the invariant wording, and the session journal, not in current output examples or shared-value definitions. |
| Phase/README structure, status, link, file-path, and test-reference audit (Python inline) | Required sections, status board, and local links pass. The audit found the Phase 2 end-to-end paths and Phase 5/6 test-file ownership notes, plus the Phase 4 and Phase 1/8 evidence notes recorded above. The first audit invocation had a syntax error; its corrected run passed the structural checks. |
| Cloud checklist capability check (`test -d`, `test -w`, and `test -x` under `$CLOUD_REPO/e2e`) | Pass; the checkout is writable and its root/e2e dependencies and Playwright executable exist. The first probe looked for Playwright in the wrong `node_modules` directory and failed. |
| `./node_modules/.bin/playwright install --list` from the public repository root | Exit 127 because the command was run from the wrong working directory; the corrected command below resolves it. |
| `cd "$CLOUD_REPO/e2e" && ./node_modules/.bin/playwright install --list` | Pass; Playwright lists installed Chromium and headless-shell browsers. |
| `git diff --check` | No output. The worklog files are untracked in this tree, so the Python trailing-whitespace audit was also used; its only hits are the two intentional prompt suffix spaces in Phase 7's exact-output block. |

The separate cloud `make -C "$CLOUD_REPO" qa` was not run in this repository-
scoped round. Its prior Phase 8 result remains recorded. The local cloud checkout
and dependencies exist; no live service or external cloud system was used.

### 2026-10-20: resolve V5-02 and V6 (session 20)

Resolved V5-02 and all seven V6 findings in the README and their owning phase
files. No phase reopened, and no source, test, or architecture file changed.
The verdict is **Ready**: V6's conditions were closed in this session. No gate
was rerun; session 19's green `make qa` remains valid on the identical code
tree (89.4% coverage, 35 npm tests). The three corrected cloud paths exist in
the local checkout; its cloud gate was not run. Stale test names remain only
in historical finding rows; `git diff --check` passed, and the worklog
trailing-whitespace audit found only the two intentional suffix spaces in
Phase 7.

### 2026-10-20: shared-config profile form recorded (session 21)

A live diagnosis showed that a named profile must be a `[profile <name>]`
section of `~/.aws/config`: the SDK ignored a bare `[slivingdoc]` section
there, while the `[slivingdoc]` section of `~/.aws/credentials` still
supplied the key, so the region stayed empty and `space` displayed
`us-east-1`. `architecture/s3store.md`, `FlagReference`, and `space -h` now
state the form, and `TestBareConfigSectionSuppliesNoRegion` proves the
adapter result without a network. No production behavior changed and no
phase reopened. `make qa` exited zero on the final tree: gofumpt, `go vet`,
staticcheck, and `go fix -diff` passed; the race suite at `-count=3` passed
within the 30 s per-package timeout; coverage was 89.4% against the 70%
floor; all 35 npm tests passed. The cloud gate was not rerun, and no cloud
file changed.

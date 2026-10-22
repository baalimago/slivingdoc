# Compatibility promise

What stays the same across releases from 1.0.0 on, what may change, and how a slivingdoc that meets something newer than it understands behaves. It answers "can I upgrade without losing notes, can an old and a new slivingdoc share a notebook, and what will break a script?"

Read this when: changing a stored format, a flag, an environment variable, a tool schema, an error token, or an exit code, adding a manifest version, or deciding whether a change needs a major release.

## Key files

| File | Purpose |
|------|---------|
| `internal/storage/manifest.go` | `ManifestVersion`, `ManifestWrapperVersion`, `ClassifyCurrent`, `DecodeManifest`, `UpgradeRequiredError`, `ErrUpgradeRequired` |
| `internal/storage/encrypted.go` | `Encrypted`, `envelopeError`: an unsupported envelope is `UPGRADE_REQUIRED`, a secret that opens no door is a key-material refusal |
| `internal/storage/key.go` | The object key grammar (`current`, `packs/checkpoints/`, `packs/increments/`) |
| `internal/workspace/state.go` | `state.json` version 1 |
| `internal/credentials/credentials.go` | `FormatVersion`, `ErrUnsupportedVersion` for `credentials.json` |
| `internal/settings/codec.go` | `FormatVersion`, `ErrUnsupportedVersion` for `workspaces.json` |
| `internal/notebook/errors.go` | The `Code`, `Reason`, `Action` tokens; `ReasonUpgradeRequired` |
| `internal/notebook/remote.go` | `manifestError`: a newer manifest is `UPGRADE_REQUIRED`, not corruption |
| `internal/app/config.go` | The flag and environment reference (`FlagReference`) |
| `internal/integrationtest/sync/upgrade_integrationtest_test.go` | The newer-manifest scenario |

## Flow

```text
any command → readRemote → read current → ClassifyCurrent / DecodeManifest
  version == 1, or no wrapper  → proceed
  version == 2 wrapper        → decorated store opens the door and reads the manifest inside;
                                 a plain reader sees version 2 as newer than 1
  version  > this build's max → UpgradeRequiredError → STORAGE_FAILURE / UPGRADE_REQUIRED
                                 (OPERATOR, not retryable)
                                 no pack read, no manifest write, L and P untouched
  version  < 1                 → ErrIntegrity → STORAGE_INTEGRITY / MANIFEST_INVALID
```

## Behavior

### The promise, from 1.0.0

These are policy for 1.x, not descriptions of code that exists today (the code has one manifest version and no multi-version reader).

Within 1.x, these do not change in a way that breaks a working setup:

- **Stored data.** Manifest version 1, the pack key grammar and the pack format stay readable and writable by every 1.x. A notebook written by any 1.x is read by every later 1.x, and one 1.x can publish over another's history. The hosted API's `/v1` routes keep their meaning ([hosted-mode.md](./hosted-mode.md)).
- **Local state.** `state.json` version 1 and `credentials.json` at `credentials.FormatVersion` stay readable. Today a `credentials.json` of another version is refused (`ErrUnsupportedVersion`) and the login is repeated; a change to either format ships with a migration or a rebuild, never a silent discard (the private state is a cache of `current`, so it can be rebuilt).
- **The tools.** `notes_pull` and `notes_commit` keep their names and the meaning of every success field. The tool input parameters are not yet declared stable: `notes_pull` takes no parameter and `notes_commit` takes only `message`, and the per-call `path` was removed without a deprecation release, as the maintainer decided on 2026-10-21. A release may add a tool parameter; once a parameter is declared stable, it is removed only in a major release. A success field may be added; no field is removed or repurposed. A success field's meaning changes only in a major release, for example `files[]` of a first pull, which is empty while its totals still count the pull's diffstat ([product-contract.md](./product-contract.md#materialization)).
- **The tokens.** `code`, `reason`, `action`, every `files[].reason` and the `retryable` rule keep their meaning. Tokens may be added; agents should treat an unknown `reason` as its `code` and follow `action`. The message text may change ([product-contract.md](./product-contract.md)).
- **The command line.** Command names and shortcuts, flag names, environment variable names, the exit codes (0 success, 1 failure) and the shape of the success line stay. A flag may be added; one is removed only in a major release, after a release that still accepts it.
- **Notebook rules.** The accepted content (valid UTF-8 text without U+0000), the path rules, the conflict marker format, and the meaning of read-only and writable paths stay; ignored names may be added ([workspace.md](./workspace.md)).

What is not promised: message wording, log record text and fields, the terminal look (colour, spinner, table layout, home screen), the exact text of the CLI report beyond its first token, timing, and the contents of the private state directory.

### An older slivingdoc meets a newer notebook

A change that older builds cannot read is a new manifest version, and only a major release writes one. A build that reads `current` and finds a version it does not know stops: `ClassifyCurrent` and `DecodeManifest` read the version first and return `UpgradeRequiredError` before they look at any other field, `manifestError` turns it into `STORAGE_FAILURE`/`UPGRADE_REQUIRED` (action `OPERATOR`, `retryable: false`) with the version found and the fix, and no pack is read, no object written and no local file changed. The notebook is intact and the old build cannot damage it, because publication starts with a read of `current` and replaces it only by ETag from that read. Upgrading that machine resolves it. Builds before this refusal existed report `STORAGE_INTEGRITY`/`MANIFEST_INVALID` for the same manifest and are equally read-only in effect. `TestScenarioUpgradeRequiredForNewerManifest` proves a hand-written wrapper of a version above this build's, and `TestScenarioEncryptedNotebookRequiresKeyMaterial` and `TestScenarioEncryptedNotebookRejectsWrongSecret` prove the version-two encrypted wrapper: a process with no secret, or with one that opens no door, refuses it before any pack read or write.

Enabling encryption is the change that writes the version-two wrapper. `encrypt init` publishes the first encrypted checkpoint, and from then on the notebook is unreadable to any older build; the transition is one-way ([encryption.md](./encryption.md)).

### A newer slivingdoc meets an older notebook

A build reads every manifest version it supports. This build reads both manifest version 1 and the version-two encrypted wrapper; it writes the wrapper only on the first encrypted publication, which is when older builds start to answer `UPGRADE_REQUIRED`. `slivingdoc version` prints the build; the release notes name any release that moves a notebook to a newer format.

## Gotchas

- `UPGRADE_REQUIRED` says nothing about the hosted service's availability. A hosted space is refused the same way when the manifest in it is newer than the client.
- A locked encrypted notebook is refused before pack access too, through `KEY_REQUIRED`, `KEY_REJECTED` or `KEY_MATERIAL_INVALID` rather than `UPGRADE_REQUIRED`; both leave the store untouched ([encryption.md](./encryption.md)).
- The promise covers what a person or an agent scripts against. Anything the [Behavior](#the-promise-from-100) list does not name (for example a log line) may change in a minor release.
- A new reason token is additive, but a new `code` is not: the seven codes in [errors.md](./errors.md) are fixed for 1.x.

## Related

- [errors.md](./errors.md), [product-contract.md](./product-contract.md), [storage.md](./storage.md), [encryption.md](./encryption.md), [workspace.md](./workspace.md), [config.md](./config.md), [releasing.md](./releasing.md)

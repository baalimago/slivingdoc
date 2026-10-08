# Phase Six: compatibility and documentation

**Status:** Not Started

README: [README.md](./README.md)

## Goal

Document the encryption contract and prove compatibility with older readers without changing gateway code.

## Specification

Add `architecture/encryption.md` in the repository's architecture-document
shape. Update each architecture document that describes changed manifest,
storage, command, configuration, error, or security behavior. Add
`scripts/check-architecture-links.py` to verify local Markdown links in the
changed architecture files. Keep the API and adapter method sets unchanged.

State the security boundary precisely: encryption protects active stored
content after conversion, not the visible workspace, private state, or pack
cache. Storage operators can still observe object keys, sizes, request timing,
publication count, and public envelope headers. This version adds no padding or
encryption-off command.
It does not erase old object versions, backups, or external copies. A human
authors passphrases, shares secrets out of band, and receives the generated
recovery code once. Do not imply key exchange, service-side recovery, or
recipient wrapping.

Preserve compatibility behavior: an older build parses the versioned
`current` wrapper, returns `UPGRADE_REQUIRED`, and does not read packs or write
objects. Document the wrapper and envelope contracts from Phases 1 and 2.

Files owned by this phase:

```text
architecture/encryption.md
architecture/README.md
architecture/cli.md
architecture/compatibility.md
architecture/config.md
architecture/errors.md
architecture/guarantees.md
architecture/hosted-mode.md
architecture/product-contract.md
architecture/releasing.md
architecture/running.md
architecture/security.md
architecture/storage.md
scripts/check-architecture-links.py
internal/integrationtest/scenario_upgrade_test.go
```

Update hosted privacy text to use the same security boundary as this phase.
Change no gateway route or code.

### Human required

The artifact is the updated hosted privacy text in the cloud repository, with that repository's own review and `make qa` result. If the repository or required approval is unavailable, stop before changing it. Complete the slivingdoc architecture and CLI documentation first, then report the blocked artifact to the maintainer.

| Invariant | Mechanism | Proof |
| --- | --- | --- |
| Older builds refuse encrypted state before pack access | Version-two wrapper and compatibility scenario | `TestScenarioUpgradeRequiredForNewerManifest` |
| Documentation does not claim local files are encrypted | Explicit scope text and status wording | `TestEncryptStatusWording` |
| Gateway code and routes remain unchanged | Compare the cloud source tree | `git -C "$CLOUD_REPO" diff HEAD --exit-code -- src/` |

## Integration contract

| Trigger | Collaborators | Observable result | Required side effects | Prohibited side effects |
| --- | --- | --- | --- | --- |
| Run an older build against encrypted `current` | Built older binary and reference store | `STORAGE_FAILURE` / `UPGRADE_REQUIRED` before pack access | None | Writes or pack reads |
| Read the architecture and command docs | Documentation tree | Format, flags, compatibility, threat limits, and errors match the implementation contract | Links resolve | Unsupported encryption claims |
| Review hosted privacy text | Cloud documentation and its quality gate | Text describes protected data and remaining metadata | Documentation-only change | Gateway route or code change |

## Acceptance criteria

| Criterion | Test or command |
| --- | --- |
| An older build returns `STORAGE_FAILURE` / `UPGRADE_REQUIRED` without touching packs or writes | `TestScenarioUpgradeRequiredForNewerManifest` |
| The new architecture document follows the repository template and all local links resolve | `python3 scripts/check-architecture-links.py` |
| The docs state the one-way mode and its remaining metadata exposure | Documentation review against this phase's security boundary |
| Changed flags, errors, storage behavior, and security limits appear in their owning docs | `git diff --check` and review against the owning phase specifications |
| Hosted privacy text avoids claims about local data or erased old copies | Cloud documentation review and that repository's `make qa` |
| No gateway code or route changes | `git -C "$CLOUD_REPO" diff HEAD --exit-code -- src/` |

## Error coverage

| Failure | Expected outcome | Test or command |
| --- | --- | --- |
| Older reader sees encrypted manifest version | `STORAGE_FAILURE` / `UPGRADE_REQUIRED` with no pack access | `TestScenarioUpgradeRequiredForNewerManifest` |
| Architecture link or required document section is missing | Documentation gate fails | `python3 scripts/check-architecture-links.py` |
| Hosted docs imply local encryption or guaranteed erasure | Review blocks the documentation change | Cloud documentation review |
| Cloud repository or approval is unavailable | Stop before that repository changes and report the missing artifact | Human-required handoff |

## Implementation notes

Not started.

## Review findings

None.
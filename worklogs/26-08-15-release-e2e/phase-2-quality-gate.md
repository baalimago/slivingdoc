# Phase 2 — Quality gate

**Status:** Not Started

**Worklog:** [README](README.md)

**Architecture:** [`../../docs/slivingdoc-v1.md`](../../docs/slivingdoc-v1.md)
§17 flags and environment; [`../../docs/testing.md`](../../docs/testing.md)
test layers (release)

## Goal

Pass the full quality gate with the new release-binary e2e in place and
update the testing documentation so the release layer description matches the
implemented coverage.

## Specification

Run the repository's single Go test command unedited:

```bash
make test
```

It must pass with the new root-package e2e included: race, three counts, the
30 s per-package budget, and the 70 % coverage floor. The root package now
starts one MinIO container per test process; confirm the three-count run
stays inside the budget on the development machine.

Run the remaining `make qa` gates and record the exact output:

```bash
go run mvdan.cc/gofumpt@v0.11.0 -w -l .
go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...
go vet ./...
go fix -diff ./...
go run github.com/mibk/dupl@v1.0.0 -t 80 .
```

`go fix -diff` must print nothing. The dupl report is a signal, not a
verdict; review any new clone in `main_e2e_test.go` against the duplication
policy and record the verdict.

Confirm the module seam scan still passes: the new test starts the binary
with `os.StartProcess`, never `os/exec` or `syscall`.

Update `docs/testing.md` in the same change: the Release layer paragraph
gains one sentence stating that the built binary now performs a real MinIO
`pull`/`commit` round trip, not only the version, help, and startup-refusal
smoke checks.

## Integration contract

| Trigger                | Collaborators                           | Observable result            | Required side effect | Prohibited side effect             |
| ---------------------- | --------------------------------------- | ---------------------------- | -------------------- | ---------------------------------- |
| `make test`            | Release binary, MinIO, libgit2          | Pass with the e2e included   | Coverage floor met   | No skip or subset                  |
| `make qa` static gates | gofumpt, staticcheck, vet, go fix, dupl | Clean, with dupl reviewed    | None                 | No `os/exec`/`syscall` import      |
| `docs/testing.md`      | Release layer paragraph                 | Describes the round-trip e2e | None                 | No stale "version/help only" claim |

## Acceptance criteria

- [ ] `make test` passes unedited with the new e2e included.
- [ ] `make qa` passes: format, staticcheck, vet, `go fix -diff` empty, and the dupl signal reviewed.
- [ ] The module seam scan still passes.
- [ ] `docs/testing.md` release-layer text matches the implemented coverage.
- [ ] The exact commands and outcomes are recorded in the implementation notes.

Each checked criterion cites the command run or the diff that proves it.

## Error coverage

| Failure                                           | Expected outcome                      | Required action                                                           |
| ------------------------------------------------- | ------------------------------------- | ------------------------------------------------------------------------- |
| Root package exceeds the 30 s budget with the e2e | `make test` fails                     | Narrow the scenario or poll instead of sleeping; do not raise the timeout |
| dupl reports a new clone                          | Review against the duplication policy | Fix actionable duplication or record the acceptable-clone verdict         |
| `go fix -diff` prints a change                    | Apply it                              | Re-run the gate                                                           |
| Seam scan flags `os/exec` or `syscall`            | Fix the import                        | Re-run `make qa`                                                          |

## Implementation notes

## Review findings

No reviews recorded.

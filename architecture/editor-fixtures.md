# Editor fixtures

`internal/editorfixture` is a test-only tool for the browser editor's TypeScript port of the pack protocol (`cloud/editor` in slivingdoc-cloud). It generates real stores with this implementation and checks any directory of manifests and packs against it.

Read this when: changing the manifest, pack export, checkpoint compaction or history validation (the committed fixtures in slivingdoc-cloud may need regenerating), or checking packs the TypeScript side wrote.

## Key files

| File | Purpose |
|------|---------|
| `internal/editorfixture/scenario.go` | `Scenario`, `Step`, `Scenarios()`: root checkpoint, increments, compaction with retained chains, delta-compressed blobs |
| `internal/editorfixture/generate.go` | `Generate`: runs the real `notebook.Notebook` over `storage/fake` and libgit2 with a fixed clock and sequential ids, dumps `current` and `packs/...` key for key, writes `expected.json` (`Expected`) |
| `internal/editorfixture/conform.go` | `Conform`, `Report`: `DecodeManifest`, `ImportPack`, `MarkShallow` and `ValidateHistory` over every `current` and `.pack` file below a directory, active and retained chains |
| `internal/editorfixture/cli.go`, `cmd/editorfixture/main.go` | `Main`: `generate <dir>` and `conform <dir>` |

## Flow

```text
generate: Pull -> per step write files, Commit -> read current for the step's head -> dump store -> expected.json
conform:  every .pack imports alone; every current: decode, per chain check size and SHA-256,
          import checkpoint, MarkShallow(checkpoint head), import increments, ValidateHistory, ReadSnapshot
```

## Behavior

- Output is byte-identical between runs for the pinned libgit2, so fixtures diff cleanly.
- `expected.json` history maps each descriptor head to its step by commit id; a compaction takes a generation of its own, so generations skip step numbers.
- `Conform` reports every failure together (`errors.Join`) and returns `storage.ErrIntegrity` wrapped for manifest and pack mismatches.

## Gotchas

- The tool needs the pinned libgit2 like every cgo package; nothing else (no Docker, no network).
- It is not part of `make qa`'s product surface; its tests run inside `make test` like any package.

## Related

[storage.md](./storage.md), [checkpoints.md](./checkpoints.md), [git-engine.md](./git-engine.md), [testing.md](./testing.md).

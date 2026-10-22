// Package itest is the toolkit of the black-box behavioral contract of the
// slivingdoc server (architecture/product-contract.md, workspace.md,
// pull.md, commit.md, conflicts.md, checkpoints.md, guarantees.md,
// config.md, security.md, and testing.md). The scenarios themselves live in
// the sibling packages cli, workspace, sync and hosted; each is a test-only
// package with its own test binary and its own 30 s deadline.
//
// The toolkit is a real (non-test) package and the only shared surface. It
// holds the harness, the scenario DSL types, the process helpers, the fault
// store, the recorder and the log capture. Scenario files dot-import it, so
// a scenario reads a helper exactly as it did before the split. Only
// `_test.go` files may import it; production code never does.
//
// The harness wires the server exactly as production startup does, through
// the internal/app constructors, with a per-test S3 prefix, workspace root,
// private root, logger, and failpoint hooks. The only entry into the black
// box is MCP JSON-RPC; scenarios never call notebook, git, workspace, or
// storage functions directly. One scenario per architecture usecase drives
// the implementation exclusively through the public MCP API: initialize,
// tool listing, and the two tool calls. The scenarios are the spec: where
// prose and a passing scenario disagree, the scenario wins.
//
// # Store selection
//
// The store seam defaults to the real s3store adapter against a pinned
// S3-compatible container, and accepts the deterministic fake and the
// fault-injecting wrappers. A scenario runs against the pinned S3 backend
// when its contract is about real HTTP conditional-write semantics — the
// CAS races, the competing checkpoint workers, the stale-reader restart,
// and the cleanup after a successful checkpoint. Every other scenario runs
// against the fake, which is contract-equivalent by construction:
// internal/storage/contract is one suite run against both the fake and the
// real S3 backend, so the fake's conditional writes, ETags, and error
// mapping are proven to match the adapter's. That keeps the whole catalog
// inside the strict race gate while the rows whose evidence must be real
// HTTP still get it.
//
// # TestMain
//
// Each scenario package's TestMain calls Main, which dispatches a spawned
// helper before m.Run, starts the shared S3 backend before the package
// deadline begins, moves the temporary directory onto a memory filesystem,
// and terminates the backend after the run.
package itest

// Package integrationtest is the black-box behavioral contract of the
// slivingdoc server (architecture sections 2 (L26), 7 (L186), 10-18
// (L603-L1115), and 20 (L1169)). One scenario per architecture usecase
// drives the implementation exclusively through the public MCP API:
// initialize, tool listing, and the two tool calls. The scenarios are the
// spec: where prose and a passing scenario disagree, the scenario wins.
//
// The harness wires the server exactly as production startup does, through
// the internal/app constructors, with a per-test S3 prefix, workspace root,
// private root, logger, and failpoint hooks. The only entry into the black
// box is MCP JSON-RPC; scenarios never call notebook, git, workspace, or
// storage functions directly.
//
// # Store selection
//
// The store seam defaults to the real s3store adapter against a pinned
// MinIO container, and accepts the deterministic fake and the
// fault-injecting wrappers. A scenario runs against MinIO when its contract
// is about real HTTP conditional-write semantics — the CAS races, the
// competing checkpoint workers, the stale-reader restart, and the cleanup
// after a successful checkpoint. Every other scenario runs against the fake,
// which is contract-equivalent by construction: internal/storage/contract is
// one suite run against both the fake and MinIO, so the fake's conditional
// writes, ETags, and error mapping are proven to match the adapter's. That
// keeps the whole catalog inside the strict race gate while the rows whose
// evidence must be real HTTP still get it.
//
// # Build tags
//
// Pure (non-CGo) files — the scenario DSL, the store recorder, the log
// capture, and the fault wrappers — carry unit tests so the CGO_ENABLED=0
// validation gate still exercises them. Everything that opens libgit2 or
// the MCP server carries the cgo build tag. Note that the CGO_ENABLED=0
// lint gate cannot see the cgo files, so dead code in a scenario file is
// not reported by staticcheck; the scenarios are kept free of it by review.
package integrationtest

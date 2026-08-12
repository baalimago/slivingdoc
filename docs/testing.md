# Testing

slivingdoc does not use live AWS resources in tests. MinIO tests use the pinned testcontainer image.

## Two commands

There is one command for Go and one for npm. Nothing else runs tests.

```text
make test
```

```text
make npm-test
```

`make test` first builds the release-style binary (the `build` target),
then runs exactly:

```text
go test -race -count=3 -timeout=30s -coverpkg=./... -coverprofile=.build/cover.out ./...
```

followed by a coverage report that fails below the 70 % floor.

The pre-build is not a second gate. It warms the exact compile cache that
the release layer's in-suite build (`release_test.go`) reuses: on a cold
runner that build alone takes about 35 s — more than the 30 s per-package
budget — so without it the gate can only pass on a machine whose cache is
already warm.

No build tag, environment variable, or flag hides part of the suite. There is
no short mode, no separate integration target, and no race-only target. Every
Go test in the repository runs on every invocation.

## Prerequisites, not options

Two dependencies are required rather than detected:

- **The pinned static libgit2.** `make test` builds it first through the
  `.build/libgit2` stamp. There is no pure-Go build: `internal/git2` needs
  CGo, so `CGO_ENABLED=0 go build` fails at compile time rather than
  producing a binary that fails at run time.
- **Docker.** The MinIO suites run against real HTTP conditional writes. An
  unreachable daemon fails the suite with an actionable diagnostic; it never
  skips. `internal/testminio` owns that policy and
  `TestRequireFailsWhenDockerIsUnavailable` pins it.

The only remaining skips are genuine platform capabilities — symlinks,
FIFOs, hard links, and Unicode normalization forms that a given OS or
filesystem cannot represent. They state the capability they need.

## Test layers

| Layer | Owner | Evidence |
| --- | --- | --- |
| Unit | All packages | Pure validation and error mapping |
| Component | `internal/git2` | Real libgit2 trees, merges, packs, and shallow history |
| Contract | `internal/storage/contract` | One ObjectStore suite against fake storage and MinIO |
| Integration | `internal/notebook`, `internal/s3store` | Publication, CAS, checkpoint, cleanup, and S3 requests |
| Scenario | `internal/integrationtest` | Black-box MCP use cases over in-memory and process transports |
| Protocol | `internal/mcp`, `internal/app` | Schemas, envelopes, stdio, configuration, and shutdown |
| Release | root package | Dependency baselines, checksum grammar, release reference, and the built binary |

The release layer drives the real `scripts/*.sh` from `release_test.go` and
builds the release-style binary once per test process, so `-count=3` links
libgit2 once.

## Other gates

Formatting and static analysis are not tests:

```text
make lint
```

Apply formatting when you change Go files:

```text
make fmt
```

`make qa` runs `lint`, `test`, and `npm-test` together.

## Why the strictness

The timeout applies to the whole three-count run of one package, so a suite
that is slow, that sleeps instead of polling, or that leaves state behind
between counts fails the gate. Scenarios therefore run with `t.Parallel()`
and take their isolation from per-test S3 prefixes and per-test workspace,
private, and cache roots.

Packages also run concurrently, including the three that each start their own
MinIO container. There is no `-p` bound: measured on a four-core runner the
slowest package sits near 20 s of the 30 s budget whether packages run one at
a time or all at once, so serializing them bought no headroom and cost about
3x wall clock.

Do not remove the race detector, the count, or the timeout, and do not
reintroduce a mode that runs a subset. A test too slow for the budget is a
design problem in the test or in the implementation.

## Coverage

70 % statement coverage is the floor and 90 % is preferred. `make test`
measures it and fails below the floor, so coverage is not a separate step.
It uses `-coverpkg=./...` because the black-box suite exercises packages
other than its own; per-package coverage would understate it badly.

To read the profile from the last run line by line:

```text
make cover
```

## Scenario rule

Start public behavior changes in `internal/integrationtest`. Use MCP
JSON-RPC as the only server entry. Add the scenario with the behavior
change. Do not change an assertion until you understand the contract that it
protects.

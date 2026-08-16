# slivingdoc v1 architecture

**Status:** Accepted implementation contract

**Product:** `slivingdoc` MCP server

**Install:** `npx slivingdoc`

## 1. Purpose

slivingdoc gives many agents one shared directory of notes. Agents use ordinary
file tools to read and edit those notes.

The product has two priorities:

1. Resolve concurrent file changes quickly.
2. Store the current notebook durably in S3-compatible object storage.

slivingdoc is not a source-control product. It uses Git data structures and
Git merge behavior internally, but it does not expose a Git repository.

The supported public API is the
[Model Context Protocol (MCP)](https://modelcontextprotocol.io/). V1 does not
provide a public Go package, a Go SDK, or a separate HTTP notebook API.

## 2. Product contract

The MCP server exposes exactly two tools:

| Tool           | Input             | Success result |
| -------------- | ----------------- | -------------- |
| `notes_pull`   | `path`            | `OK`           |
| `notes_commit` | `path`, `message` | `OK`           |

The normal workflow is:

```text
notes_pull(path)
        |
        v
edit UTF-8 text files at path
        |
        v
notes_commit(path, message)
```

`notes_pull` writes the current notebook into `path`. `notes_commit` publishes
the caller's changes and incorporates concurrent, non-conflicting changes.

The same two operations are also public as one-shot process subcommands, so
a human shares the directory with the agents without an MCP host:

```text
slivingdoc pull <path>
slivingdoc commit <path> -m <message>
```

A subcommand `path` can be relative. It resolves against the working
directory, and the resolved path must stay at or below the workspace root.
A successful subcommand writes the unified result report to stdout and
exits zero: the `OK` status token, the accepted remote generation, one
line per changed file with its insertion and deletion counts, and the
totals trailer. A domain error writes the same status/detail/trailer
skeleton as a candid text report to stdout and exits nonzero. The report
contains the category and message, the retryable verdict, one line per
conflicted file with its one-based inclusive line ranges, and the recovery
report when present. Colour is presentation-only: the status tokens, the
generation summary, and the per-file counts are coloured only when stdout
is a real terminal, and any non-empty `NO_COLOR` disables the colour even
there. The report carries the same categories, the same relative file
paths, and the same redaction guarantees as the MCP envelope.

The caller never receives a Git object ID, pack name, S3 key, or local checkout
path. These values are internal implementation details.

The commit message is retained in recent internal Git data. V1 does not promise
permanent message or commit-history retention.

Tool inputs are strict JSON objects. `notes_pull` requires only `path`.
`notes_commit` requires only `path` and `message`. Unknown fields and explicit
null values are invalid. `path` is an absolute UTF-8 host path with 1 through
4,096 bytes. `message` is valid UTF-8 with at most 16,384 bytes and no U+0000.
The service rejects a message that contains only Unicode white space. It
preserves every byte of any other message.

A successful tool result contains one MCP text item with exactly `OK` and
this structured object:

```json
{
  "code": "OK",
  "generation": 18,
  "filesChanged": 3,
  "insertions": 3,
  "deletions": 4,
  "files": [
    { "path": "notes/a.md", "insertions": 1, "deletions": 1 },
    { "path": "notes/c.md", "insertions": 2, "deletions": 0 },
    { "path": "archive/old.md", "insertions": 0, "deletions": 3 }
  ]
}
```

`code` is always `OK`; `generation` is the accepted remote generation
after the operation; `filesChanged`, `insertions`, and `deletions` are the
totals of the per-file change stat; `files` is always present, empty for a
no-op synchronization. The pull diffstat is the on-disk delta between the
visible state before the pull and the materialized result; the commit
diffstat is the increment the publication added over the observed remote
parent tree, empty for a no-op synchronization. Paths are the same
normalized internal slash form used by error files, and no success data
contains credentials, S3 keys, private paths, or Git IDs.

A diffstat line is an LF-terminated run of bytes with one trailing CR
stripped for comparison and counting. A final run without a trailing LF
still counts as one line; content ending in LF has no phantom empty final
line; empty content has zero lines. A file present only after the change
counts every line as an insertion; a file present only before the change
counts every line as a deletion; a modified file uses a deterministic
line diff.

A domain error returns an MCP tool result with `isError=true`, one candid
text item, and this structured object:

```json
{
  "code": "CONTENT_CONFLICT",
  "retryable": false,
  "message": "Resolve the conflict blocks before notes_commit.",
  "files": [
    {
      "path": "notes/today.md",
      "ranges": [{ "start": 12, "end": 18 }]
    }
  ]
}
```

`code`, `retryable`, `message`, and `files` are always present. Paths are
normalized internal paths. Ranges are one-based, inclusive, ordered, and
non-overlapping. A file without a marker range has an empty `ranges` array.
`RECOVERY_FAILURE` also includes `recovery` with string `stage`, enum
`remoteAccepted` (`yes`, `no`, or `unknown`), and Boolean `resynchronized`.
No error text or data contains credentials, S3 keys, private paths, or Git IDs.
Request `path` is absolute. Every `files[].path` in an error is relative to that
request path and uses the normalized internal slash form.

## 3. Scope

V1 includes:

- stdio MCP transport
- direct `pull` and `commit` subcommands for humans
- one shared notebook per configured server
- UTF-8 text files and directories
- text merges with visible conflict markers
- S3-compatible durable storage
- optimistic concurrent publication
- automatic count-based checkpoints
- a self-contained native executable
- an npm launcher that downloads the correct executable

V1 does not include:

- a Git executable dependency
- `git-remote-s3`
- `git2go`
- a public Git remote
- a writer lock or lease object in S3
- symlinks, devices, sockets, named pipes, or hard-link semantics
- branch, tag, ref, revision, checkout, or rollback APIs
- permanent Git history
- an application-level backup service

## 4. Terms

| Term              | Meaning                                                                                                         |
| ----------------- | --------------------------------------------------------------------------------------------------------------- |
| Local state (L)   | The caller-controlled visible directory. Agents and humans edit it, and slivingdoc can rewrite it during calls. |
| Private state (P) | Slivingdoc-owned local Git data, object cache, accepted baseline, merge scratch state, and conflict metadata.   |
| Accepted baseline | The remote state recorded in P as the base for the unpublished changes currently represented by L.              |
| Remote state (R)  | The accepted notebook state indexed by S3 `current`. Unreferenced S3 objects are not part of R.                 |
| Pack              | An immutable Git pack file that contains Git objects.                                                           |
| Increment         | A pack that contains objects for one accepted publication and can depend only on the indexed packs before it.   |
| Checkpoint        | A closed pack that reconstructs a complete notebook state without older packs or external base objects.         |
| Manifest          | The S3 `current` object that indexes the accepted state.                                                        |
| Generation        | The monotonic publication number in the manifest.                                                               |
| CAS               | A conditional update that succeeds only when the observed S3 ETag still matches.                                |

## 5. System overview

```text
                         MCP client
                             |
                 notes_pull / notes_commit
                             |
                             v
                 +-----------------------+
                 | slivingdoc MCP server |
                 +-----------+-----------+
                             |
                  notebook orchestration
                    /                 \
                   v                   v
       +----------------------+   +----------------------+
       | private local state  |   | S3 storage protocol  |
       |                      |   |                      |
       | visible-file mirror  |   | current manifest     |
       | libgit2 object cache |   | immutable Git packs  |
       +----------+-----------+   +----------+-----------+
                  |                          |
                  v                          v
       caller-supplied path       S3-compatible bucket
```

The Go process uses the
[AWS SDK for Go v2](https://docs.aws.amazon.com/sdk-for-go/v2/developer-guide/getting-started.html)
for all S3 requests.
It does not ask libgit2 to use a network transport.

The process calls a narrow internal CGo wrapper around
[libgit2](https://libgit2.org/). libgit2 supplies local Git object, pack, tree,
commit, index, and three-way merge behavior.

The process never invokes a Git command. Users do not need Git, libgit2, a C
compiler, or CGo on their machines.

## 6. Why Git and S3 are combined

Git and S3 have separate responsibilities.

| Component  | Responsibility                                                                                |
| ---------- | --------------------------------------------------------------------------------------------- |
| libgit2    | Build trees and commits, create and import packs, and merge three file trees.                 |
| S3         | Store immutable bytes, expose the accepted manifest, and enforce conditional publication.     |
| slivingdoc | Apply product policy, manage paths, retry contention, show conflicts, and create checkpoints. |

This design uses mature Git merge behavior without operating a Git server. It
also avoids a mounted object-store filesystem and a separate coordination
database.

Most work happens concurrently. Only the final conditional replacement of the
small `current` object orders successful publications.

## 7. Public filesystem model

### 7.1 UTF-8 text files only

The notebook contains directories and regular UTF-8 text files. Empty files are
valid. File content must be valid UTF-8 and must not contain U+0000. Slivingdoc
preserves file bytes and line endings; it does not normalize text content.
Paths use slash-separated relative names in internal data.

The service rejects:

- symbolic links
- hard-link semantics
- sockets
- named pipes
- device files
- invalid UTF-8 or U+0000 in file content
- paths that escape the configured workspace root
- file and directory names that cannot be represented safely on the host

Executable bits and other platform-specific modes are not notebook state.
Files have one normal file mode when slivingdoc writes them.

Empty directories are not notebook state because Git does not store them.
Materialization creates required parent directories and removes obsolete empty
directories. Hard-linked regular files are read as independent paths. A rewrite
does not preserve the hard link.

Each internal path is valid UTF-8 in Unicode NFC form. It uses `/` separators
and has at most 4,096 UTF-8 bytes. Each segment has 1 through 255 UTF-8 bytes.
A segment cannot contain a control character or `/\:*?"<>|`. It cannot end in
a space or dot. The service rejects `.`, `..`, `.git`, Windows device names,
and names that collide under Unicode case folding. These rules apply on every
host, so one accepted notebook is portable across supported hosts.

Files use libgit2's normal text merge behavior. Binary files and other content
that fails the text-file rule are not notebook state.

A file-versus-directory replacement is a content conflict when both sides
depend on the same path.

### 7.2 Visible and private directories

The local visible directory L contains no `.git` directory. Private state P
contains the Git object cache and accepted baseline for L.

```text
/workspace/notes/                 visible and caller-owned
  topic-a.md
  agents/observations.md

<private-root>/<derived-key>/     private and server-owned
  repository data
  path baseline
  conflict metadata
```

On POSIX hosts, slivingdoc creates P directories with mode `0700` and P files
with mode `0600`. It writes L directories with `0755` and L files with `0644`,
subject to the process umask. Windows inherits the root ACL and never broadens
it.

The derived key includes the canonical visible path and the notebook storage
identity. It uses a digest and does not expose the caller's path.

The storage identity contains the normalized S3 endpoint, region, bucket,
prefix, and manifest version. The private-directory key is the lowercase
SHA-256 of a length-prefixed encoding of the canonical L path and storage
identity. It is not a concatenation with ambiguous separators.

P stores a strict `state.json` version 1 record with these fields in order:
`version`, `identity`, `remoteGeneration`, `baselineHead`, `baselineTree`, and
`recoveryRequired`. The identity and tree are lowercase SHA-256 and Git object
IDs as applicable. `baselineHead` is empty only at remote generation 0. The Git
tree in P is the authoritative baseline. A file snapshot is only a cache.
Slivingdoc writes this record with a temporary file, file sync, and atomic
rename. It rejects unknown, duplicate, missing, or null fields.

The canonical empty Git tree is
`4b825dc642cb6eb9a060e54bf8d69288fbee4904`. A new P state uses remote
generation 0, an empty baseline head, this baseline tree, and
`recoveryRequired=false`.

Before it starts any L replacement, slivingdoc persists
`recoveryRequired=true`. After replacement and baseline update are durable, it
persists the final state with `recoveryRequired=false`. A process restart with
the flag set discards local intent and reconstructs P and L from `current`
before normal work.

Calls for one L path are serialized inside one process. Different paths can
prepare work concurrently. A local operation lock prevents two server processes
from changing the same P state at the same time.

### 7.3 Operation-boundary synchronization

V1 does not use a background filesystem watcher. It ingests and rewrites L only
at MCP operation boundaries.

The MCP `path` is an absolute host path. The service cleans it lexically and
requires it to be at or below the absolute workspace root. Filesystem access
uses Go `os.Root` relative operations. It does not use a check-then-open path
sequence. Existing or newly substituted symlink components cannot escape the
root. The service creates L when it does not exist.

This rule avoids races with partial editor writes and atomic rename patterns.
The caller edits files only between tool calls.

`notes_pull` treats differences between the accepted baseline in P and L as
unpublished local changes. It merges those changes onto R and rewrites L with
the result. A conflict writes marker content to L and returns a content-conflict
error; pull does not restore the pre-call bytes.

If P has no accepted baseline, pull uses the canonical empty tree. Existing
valid text files in L are local additions and merge with R. An add/add conflict
at the same path uses the normal conflict behavior.

All valid files below L are notebook state. V1 has no ignore file. The caller
must not modify L while an MCP operation for that path is active.

## 8. Internal Git model

### 8.1 Narrow CGo boundary

All CGo code stays in one internal package. No C pointer or libgit2 type crosses
that package boundary.

The boundary provides only the operations that slivingdoc needs:

- open or initialize a local repository
- create and inspect blobs, trees, and commits
- create a tree from regular files
- merge three trees
- enumerate merge conflicts
- write conflict markers
- create an incremental pack against an explicit indexed base
- create a closed checkpoint pack with no missing base objects
- import and validate a pack
- read object IDs and tree contents

The wrapper pins one tested libgit2 release, v1.9.6. The release build compiles libgit2
without SSH or Git HTTP transports because S3 access belongs to the Go process.

For background, see the
[CGo command documentation](https://pkg.go.dev/cmd/cgo),
[libgit2 merge API](https://libgit2.org/docs/reference/main/merge/git_merge_trees.html),
and [libgit2 packbuilder API](https://libgit2.org/docs/reference/main/pack/git_packbuilder.html).

### 8.2 Three-tree merge

Each pull or commit compares three complete file trees:

```text
base A    accepted baseline recorded in private state P
local L   files now present in the caller-controlled directory
remote R  latest accepted S3 state
```

The merge computes:

```text
local change  = A -> L
remote change = A -> R
result        = merge(A, L, R)
```

If changes affect different files or different compatible lines, libgit2
produces one merged tree. The server creates one commit whose parent is `R`.

All notebook blobs use Git mode `100644`. Directory trees use `040000`.
Slivingdoc rejects every other mode. It sorts path components by Git tree order
before tree creation. Commit author and committer are
`slivingdoc <slivingdoc@localhost>`. Commit time is the operation-attempt start
time in UTC with offset zero and one-second precision. The initial commit has no
parent. Every later commit has exactly the observed R head as its parent.

Tree merge does not calculate a history merge base. It passes the explicit
baseline, local, and remote trees to libgit2. Merge labels are `local` and
`remote`. No external merge driver, Git configuration, or working-tree
attribute changes product behavior.

Git commit history is an internal aid for recent ancestry and pack creation.
It is not the durable product contract.

### 8.3 Local object cache

Each private repository retains imported objects. A normal pull imports only
packs that are absent from that cache.

P stores downloaded pack bytes by lowercase SHA-256, not by an untrusted S3
key. A cache hit requires the expected byte size and a fresh SHA-256 check.
Downloads use a temporary file and atomic rename. After all imports, the Git
engine walks the accepted head commit, complete tree, and every blob. Pull does
not rewrite L until this closure and all text content pass validation.

The cache improves performance but does not determine accepted state. The S3
manifest and its referenced packs are authoritative.

A new process can rebuild a repository from the active checkpoint and tail.
The service can reset refs and visible files without deleting reusable objects.

## 9. S3 storage model

### 9.1 Object layout

One configured prefix contains one notebook:

```text
<prefix>/current
<prefix>/packs/checkpoints/<through-generation>-<checkpoint-id>.pack
<prefix>/packs/increments/<generation>-<publication-id>.pack
```

Pack keys contain their target or through-generation and a unique publication
or checkpoint ID. Both IDs are UUIDv7 values encoded in canonical lowercase
RFC 9562 text form (`8-4-4-4-12` hexadecimal characters). Validation requires
version 7 and the RFC 4122 variant. Generation fields, not UUID ordering, define
protocol order. One writer owns each key, and slivingdoc never changes accepted
pack bytes.

The configured prefix is empty or a slash-separated relative key prefix. It
has no leading slash, trailing slash, empty segment, backslash, `.` segment, or
`..` segment. Configuration rejects an invalid prefix. The object-store adapter
joins a nonempty prefix and a protocol key with one slash.

Every pack upload writes `slivingdoc-sha256`, `slivingdoc-size`,
`slivingdoc-kind`, and `slivingdoc-generation` S3 user metadata. The kind is
`increment` or `checkpoint`. Metadata helps diagnose and resume uploads, but
the manifest descriptor is authoritative. Reuse of an existing unique key
requires a streamed GET that proves its size and SHA-256.

Small packs can use one object upload. Large checkpoint packs can use S3
multipart upload. A retry first inspects an object at the same unique key. It
accepts existing bytes only when the recorded SHA-256 and size match.

S3 does not contain a bare repository or a `.git` directory. The server does
not use S3 `LIST` to decide which notebook state is current.

### 9.2 The `current` manifest

`current` is the only authoritative state index. It names one active checkpoint
and every accepted increment after that checkpoint.

A manifest version 1 has this normative shape:

```json
{
  "version": 1,
  "generation": 8121,
  "head": "<git-object-id>",
  "checkpoint": {
    "id": "<checkpoint-id>",
    "publication": "<publication-id-of-head>",
    "throughGeneration": 8119,
    "head": "<git-object-id>",
    "key": "packs/checkpoints/8119-<checkpoint-id>.pack",
    "sha256": "<pack-sha256>",
    "size": 123456
  },
  "increments": [
    {
      "generation": 8120,
      "publication": "<unique-publication-id>",
      "parent": "<git-object-id>",
      "head": "<git-object-id>",
      "key": "packs/increments/8120-<publication-id>.pack",
      "sha256": "<pack-sha256>",
      "size": 4096
    }
  ],
  "retained": [
    {
      "retiredAtGeneration": 8121,
      "head": "<retained-head>",
      "checkpoint": {
        "id": "<checkpoint-id>",
        "publication": "<publication-id-of-head>",
        "throughGeneration": 7095,
        "head": "<git-object-id>",
        "key": "packs/checkpoints/7095-<checkpoint-id>.pack",
        "sha256": "<pack-sha256>",
        "size": 123456
      },
      "increments": []
    }
  ]
}
```

The field names shown here are normative for manifest version 1. Version 1
contains these properties:

- a required integer `version` field with value `1`
- one monotonic unsigned 64-bit generation
- the accepted head object ID
- one active checkpoint descriptor
- an ordered incremental tail
- checksums and sizes for every pack
- retained generations that each contain the previous checkpoint descriptor,
  its complete ordered tail through the replaced cutoff, and its accepted head

The manifest is bounded by automatic checkpoints. With the default threshold,
it contains at most approximately 1,024 active increments during normal use.

`generation` and every generation or cutoff field are unquoted JSON integers
decoded as Go `uint64`. An absent `current` is the implicit remote-empty state
at generation 0. The first successful conditional creation writes
generation 1. Every successful replacement of `current`, including a
checkpoint replacement, increments the observed generation by exactly one.
Generation values never reset. Overflow rejects the operation.

Manifest version 1 rejects an unknown field, a duplicate field name, a missing
required field, and explicit JSON `null` at every object level. Validation of
duplicate names occurs before values are decoded into Go structures. A future
incompatible or extended schema uses a different `version` value; version 1
readers reject it before any referenced pack is accessed.

The encoder writes compact UTF-8 JSON with no trailing newline. It writes object
fields in the order shown in the normative shape. It uses Go `encoding/json`
string escaping with HTML escaping disabled. Arrays retain their protocol
order. Writers do not use maps to encode protocol objects.

The validator applies these rules to a stored manifest before it accesses a
pack. An absent `current` is not a manifest and uses the generation 0 rule.

1. `generation` is at least 1.
2. Pack `size` is a positive `uint64`.
3. Every object key and checkpoint ID is unique across the manifest.
4. The active checkpoint cutoff is not greater than `generation`.
5. Active increment generations increase and are greater than the checkpoint cutoff.
6. The first active increment parent equals the active checkpoint head.
7. Each later increment parent equals the preceding increment head.
8. The top-level `head` equals the last increment head, or the checkpoint head when the tail is empty.
9. A checkpoint key is `packs/checkpoints/<throughGeneration>-<id>.pack`.
10. An increment key is `packs/increments/<generation>-<publication>.pack`.
11. A key is relative and contains no backslash, empty segment, `.` segment, or `..` segment.
12. Each retained generation validates as an independent checkpoint and tail chain.
13. `retained` is ordered from newest to oldest by decreasing `retiredAtGeneration`.
14. A retained head equals its final increment head, or its checkpoint head when its tail is empty.
15. Each retained retirement generation is greater than its final content generation and not greater than top-level `generation`.

Publication IDs are unique within each reconstructable chain. Two chains can
repeat a publication ID only when both descriptors bind it to the same commit
head. This repetition occurs when an active checkpoint copies the final
publication ID from the retained increment that it compacted.

Each checkpoint descriptor carries the publication ID of its head commit. A
checkpoint that compacts an increment copies that increment publication ID.
An ambiguous commit searches the active and retained checkpoint and increment
descriptors. The manifest has no separate publication-receipt collection.

### 9.3 Pack integrity

V1 repositories use Git's SHA-1 object format. Every commit, tree, blob, head,
and parent object ID is encoded as exactly 40 lowercase hexadecimal characters.
Format 1 does not accept another object-ID algorithm or representation.

The Git object IDs protect Git object content. Each manifest descriptor also
contains a SHA-256 checksum and byte size for the complete pack. The pack
checksum is encoded as exactly 64 lowercase hexadecimal characters.

The server validates both before it imports a downloaded pack. A mismatch is a
storage-integrity error. The server does not advance or materialize corrupt
state.

An S3 ETag is only a concurrency token for `current`. It is not a content
checksum. Multipart-upload ETags are not reliable content digests.

Checkpoint packs are closed for state reconstruction. They contain the
checkpoint commit and all tree and blob objects required for that state. They
use no pack delta whose base object exists only in an older pack.

The checkpoint commit can name an intentionally omitted parent commit. That
parent is history, not required file state. The manifest identifies the
checkpoint commit as a shallow boundary, and validation permits only that
declared history gap.

An incremental pack can omit objects already present in the exact checkpoint
and tail indexed as its base. Its descriptor records that parent state. Import
validation must fail when any required object is unavailable.

### 9.4 S3 compatibility

The deployment supplies a bucket, prefix, region, endpoint, and credentials.
Credentials use the AWS SDK default credential chain.

Runtime credentials require object GET, PUT, DELETE, multipart upload, and
multipart abort permissions below the configured prefix. Cleanup also requires
bucket LIST restricted to that prefix. Slivingdoc does not create or configure
the production bucket.

Before serving MCP calls, the server runs a disposable compatibility probe. It
proves that the store enforces:

- `If-None-Match: *` for conditional creation
- `If-Match: <etag>` for conditional replacement
- read-after-write behavior required by the protocol

The probe uses a unique `probe/<uuidv7>` key below the configured prefix. It
conditionally creates known bytes, proves that a second create fails, reads the
bytes and ETag, proves that a wrong ETag fails without mutation, replaces with
the correct ETag, and reads the replacement immediately. It deletes the probe
key on success and after any recoverable failure.

The first manifest write uses `If-None-Match: *`. A later write uses the exact
ETag returned by the preceding GET in `If-Match`. A successful write stores
`application/json` content with the version 1 encoding. HTTP 412 maps to the
semantic precondition error. A timeout or connection error after request bytes
were sent is an ambiguous result, not a precondition failure.

See the
[S3 conditional-write documentation](https://docs.aws.amazon.com/AmazonS3/latest/userguide/conditional-writes.html).

Bucket versioning is optional. Versioning, replication, object lock, lifecycle
rules, and external backups are deployment recovery policies. Publication
correctness does not depend on them.

## 10. Pull operation

`notes_pull(path)` performs this logical sequence:

```text
validate and lock L and P
              |
              v
validate and ingest text files from L
              |
              v
GET current and validate the manifest
              |
              v
download missing checkpoint or increment packs
              |
              v
validate and import packs into P
              |
              v
merge accepted baseline, L, and R
              |
              v
rewrite L; record R as the new baseline in P
```

Pack downloads can occur concurrently. State reconstruction follows the order
recorded in the manifest.

If an indexed pack disappears during cleanup, the reader discards its stale
observation and reads `current` again. It never guesses state from object names.
If the new ETag and manifest are unchanged, the referenced missing pack is a
`STORAGE_INTEGRITY` error. The reader does not retry the same manifest forever.

If the merge conflicts, pull writes non-conflicting results and text conflict
markers to L, records R as the new baseline in P, and returns exact conflict
paths and marker ranges. It does not revert L.

Pull always materializes the full merge result. A conflicted path contains its
marker content. Every other path contains the clean merged content.

If `current` does not exist, R is the canonical empty tree. Pull retains valid
local additions in L, records the empty tree as its baseline in P, and does not
create remote state.

## 11. Commit and optimistic publication

### 11.1 Normal commit

`notes_commit(path, message)` requires a non-blank message and accepted baseline
state in P. Before any Git or S3 publication work, it validates all files in L
and rejects every complete conflict-marker block. The error identifies each
normalized path and marker row range. Literal marker examples must change at
least one marker signature before they can be committed.

The caller must run `notes_pull` once for an L path before its first commit.
Commit does not create P from an unknown L path. It returns `INVALID_REQUEST`
without S3 mutation when the baseline is absent.

The operation performs this logical sequence:

```text
read and validate text files from L
            |
            v
reject complete conflict-marker blocks
            |
            v
build local tree and read R with its ETag
            |
            v
merge(A, L, R) with libgit2
            |
       +----+----+
       |         |
       v         v
   conflict   merged tree
       |         |
       |         v
       |    create commit and incremental pack
       |         |
       |         v
       |    upload immutable pack
       |         |
       |         v
       |    PUT current with If-Match
       |         |
       |    +----+----+
       |    |         |
       v    v         v
    error  accepted  ETag changed
                    merge latest and retry
```

After acceptance is proved, commit rewrites L to the exact accepted merged tree
and records that tree and head as the new baseline in P before returning `OK`.
If the merged result already equals R, commit performs the same local
synchronization without a remote mutation. It creates no publication ID, pack,
commit, or CAS request for this no-change result.

If the notebook is empty, the first publication creates `current` with
`If-None-Match: *`.

The first publication creates a root commit and a state-complete checkpoint
pack. Its manifest has that checkpoint and an empty incremental tail.

Only the `current` update accepts a proposal. Uploading a pack does not publish
it.

### 11.2 Compare and swap

Many writers can read the same manifest, merge, and upload packs concurrently.
S3 accepts only one replacement for the observed ETag.

```text
current A, ETag e1

writer 1: A -> B, PUT If-Match e1  ---- accepted ----> current B, ETag e2
writer 2: A -> C, PUT If-Match e1  ---- rejected
writer 2: merge C with B
writer 2: B -> D, PUT If-Match e2  ---- accepted ----> current D
```

A failed comparison is expected contention, not a storage failure. The writer
downloads missing increments, merges against the new head, waits with bounded
randomized backoff, and retries.

There is no `LOCK.lock` object. There is no lease, renewal loop, stale-lock
recovery, or clock-based lock expiry.

### 11.3 Retry and uncertain responses

Retries have fixed configurable bounds. Exhaustion returns an error and keeps
the caller's files available for another attempt.

Each proposal has a unique publication ID. If the connection fails during the
manifest update, the server reads `current` before it decides the result.

If the publication ID is present in an active or retained checkpoint or
increment descriptor, the commit succeeded. If the service cannot find the ID
after those descriptors expire, it returns a storage failure, preserves the
visible workspace, and does not automatically republish the proposal. The
operation can have succeeded remotely even though the service cannot prove the
result.

The service never returns `OK` when acceptance is uncertain.

An unreferenced proposal pack is an orphan. It does not affect notebook state.
Checkpoint cleanup can remove it only after a successful checkpoint cutoff has
reached its declared target generation.

After a commit CAS precondition failure, the retry reads the new manifest and
creates a new attempt with a new publication ID, target generation, pack key,
commit, and pack. It never publishes the losing attempt's pack against a later
generation.

Pack-upload ambiguity is separate from manifest ambiguity. If a pack upload
loses its response, the server reads its unique key and validates SHA-256 and
size. It reuses matching bytes and never treats the pack alone as publication.

## 12. Conflict behavior

Libgit2 returns conflicts through a merge index. The server uses those entries,
not a global text scan, to identify conflicted files.

For text conflicts, the visible file contains standard markers:

```text
<<<<<<< local
the caller's text
=======
the accepted remote text
>>>>>>> remote
```

The MCP error contains stable structured data:

```json
{
  "code": "CONTENT_CONFLICT",
  "files": [
    {
      "path": "notes/today.md",
      "ranges": [{ "start": 12, "end": 18 }]
    }
  ]
}
```

The operation does not update `current`. It writes all non-conflicting merge
results and conflict files to the visible directory.

The caller resolves the files with normal file tools and calls `notes_commit`
again. The server records the accepted remote state used for the conflict.

On retry, the resolved directory becomes the caller's local intent. If remote
state moved again, the server performs another three-tree merge.

`notes_commit` never accepts a complete conflict-marker block, including one not
created by the current process. This content rule survives process restart and
can reject literal marker examples. The conflict error names every affected
path and marker row range.

A complete marker block contains these exact lines at column zero and in this
order: `<<<<<<< local`, `=======`, and `>>>>>>> remote`. The scanner accepts LF
and CRLF line endings and ignores the line terminator during comparison. It
finds all complete, non-nested blocks in every file. Row numbers are one-based
and inclusive. Changing one character in any signature makes that block
ordinary text.

## 13. Automatic checkpoints

### 13.1 Purpose

Incremental packs make normal commits small. An unlimited incremental chain
makes cold startup slow and request-heavy.

A checkpoint contains the complete file state at one accepted head. A new
server needs one checkpoint plus only the increments after it.

The default checkpoint threshold is 1,024 increments. The threshold is
configurable because commit sizes and network conditions vary.

V1 triggers checkpoints by pack count. Metrics expose tail bytes so a later
version can add a byte threshold without changing the storage format.

After a commit makes the active tail length greater than or equal to the
configured threshold, it schedules one checkpoint effort for that observed
manifest. Retained tails do not count. The worker selects the oldest threshold
increments as its stable prefix. The selected cutoff is the generation of the
last selected increment.

### 13.2 Shallow history

A checkpoint preserves the exact checkpoint head commit and its complete tree.
It omits older commit ancestors, but it omits no tree or blob needed by the
checkpoint state. It has no unresolved pack-delta base.

When the pack enters a local repository, slivingdoc records the checkpoint head
as a shallow history boundary. Later incremental commits can use that exact
head as their parent.

Thus, a checkpoint preserves current file state and supports later increments.
It does not preserve permanent Git history.

The checkpoint pack contains the checkpoint commit, its complete tree closure,
and every referenced blob. It excludes tags, refs, and commit ancestors before
the shallow boundary. Importing this pack alone into an empty repository must
make the checkpoint commit and complete file tree readable.

```text
before checkpoint

C0 -> I1 -> I2 -> ... -> I1024 -> I1025 -> I1026
      \________ compacted ______/       \__ tail __/

after checkpoint

C1(at I1024) -> I1025 -> I1026
```

### 13.3 Stable-prefix compaction

Checkpoint creation does not block normal writers. A worker builds a checkpoint
for an immutable accepted prefix while newer increments continue.

When the checkpoint is ready, the worker reads the latest manifest. If that
manifest still contains the compacted prefix, it replaces only that prefix and
keeps every later increment.

The proposal generation is the latest manifest generation plus one. The new
checkpoint through-generation is the selected cutoff. Its head and publication
ID equal the final compacted increment. The replaced checkpoint and compacted
increments become one retained entry with `retiredAtGeneration` equal to the
proposal generation. Existing retained entries follow it from newest to oldest.
The writer then trims that array to the configured retention count.

The final manifest replacement uses the normal ETag CAS. A CAS failure requires
another small manifest rewrite, not another checkpoint-pack build.

The worker reuses its checkpoint pack only while the latest active descriptor
chain still contains its exact selected prefix. A normal commit preserves that
prefix. A checkpoint at or beyond the selected cutoff removes it, so the worker
must discard its proposal and cannot reference that pack in a later manifest.

Competing checkpoint workers are safe. At most one manifest replacement wins.
The losing checkpoint is an unreferenced object and can be cleaned later.

Checkpoint failure never changes the result of an already accepted commit.

## 14. Retention and cleanup

slivingdoc promises current-state durability. It does not promise historical
recovery.

The manifest retains the active checkpoint generation and one previous
checkpoint generation by default. The retention count is configurable. Each
retained generation contains its checkpoint descriptor, complete ordered
increment tail through the cutoff that replaced it, and accepted head. It can
reconstruct the exact accepted state that the newer checkpoint replaced.

The retention configuration counts previous generations in addition to the
active generation. Its default is 1. A value of 0 retains only the active
generation. A checkpoint update keeps the newest configured number of retained
entries and removes older entries from the root set.

When a later checkpoint succeeds, it performs best-effort cleanup of physical
objects older than the retained generations:

```text
C0 becomes C1  -> retain C0 and C1 storage
C1 becomes C2  -> retain C1 and C2, then clean C0 storage
```

Cleanup occurs only after the new manifest CAS succeeds. Cleanup failure does
not fail a commit or checkpoint. A later checkpoint can retry it.

Readers never depend on a download timeout for safety. If a stale reader finds
a removed pack, it reads the newer `current` manifest and restarts.

The active and retained manifest data are the garbage-collection roots. Cleanup
considers only objects whose target or through-generation is at or before the
stable cutoff of the successful checkpoint that triggered cleanup. It retains
every candidate referenced by active or retained manifest data. It can delete
any other candidate, including a retired pack or never-accepted proposal. It
never considers an object after the cutoff.

Every publication writes all pack bytes before it writes the referencing
manifest. A pack alone is only a proposal. The manifest CAS is the atomic
acceptance action for both increments and checkpoints.

Cleanup must reread `current` before deletion and apply its configured retained-
generation roots. It must never delete a pack referenced by active or retained
manifest data. A stale reader can restart from the latest manifest, so the
protocol does not require reader leases.

Cleanup runs only after a successful checkpoint CAS. It lists only the
`packs/checkpoints/` and `packs/increments/` namespaces. It parses the generation
from each valid protocol key and ignores malformed keys. It considers only keys
at or before the successful checkpoint cutoff. Before each delete batch, it
rereads and validates `current` and rebuilds the active and retained key set.
The `current` key is never a deletion candidate. Multipart upload cleanup is
part of upload handling, not checkpoint garbage collection.

An orphan proposal after the current cutoff stays untouched. A later
checkpoint cutoff eventually makes its generation eligible. If checkpoints do
not succeed, cleanup does not run and old proposals remain stored.

Cleanup follows every LIST continuation token. It sends at most 1,000 keys in
one delete request. It records each per-key delete error and retries it only on
a later checkpoint cleanup.

S3 versioning can retain deleted object versions. Deployment lifecycle rules
determine when those noncurrent versions consume no more storage.

## 15. Failure guarantees

The publication order is load-bearing:

```text
create local state
    -> upload immutable pack
        -> conditionally replace current
```

| Failure point                 | Guaranteed result                                                        |
| ----------------------------- | ------------------------------------------------------------------------ |
| Before pack upload            | Remote state is unchanged.                                               |
| During pack upload            | Remote state is unchanged.                                               |
| After pack upload, before CAS | The pack is an unreferenced proposal.                                    |
| Pack upload response lost     | Read the unique key and validate SHA-256 and size.                       |
| CAS precondition failure      | Another writer won. Merge and retry.                                     |
| CAS response lost             | Read `current` and search for the publication ID.                        |
| Merge conflict                | Remote state is unchanged. L is rewritten with the merge and markers.    |
| Retry exhaustion              | Remote result is not reported as success. Caller files remain available. |
| Checkpoint failure            | Accepted notebook state is unchanged.                                    |
| Cleanup failure               | Accepted notebook state is unchanged. Obsolete storage remains.          |
| Corrupt pack or checksum      | Refuse import and report a storage-integrity error.                      |

`notes_commit` returns `OK` only after the server proves that `current` accepted
the proposal or a later manifest records its publication ID.

The implementation does not prescribe a separate recovery algorithm for every
possible interruption. If an unexpected failure occurs after local mutation
starts, the operation stops normal work, reports `RECOVERY_FAILURE`, and tries
to reconstruct P and L from authoritative `current`. It reports the failed
stage, whether remote acceptance is known, and whether resynchronization
succeeded. A successful repair does not convert the anomalous call to `OK`.

If immediate repair is impossible, P records that recovery is required. The
next MCP call must retry authoritative resynchronization before it performs new
pull or commit work. Recovery can replace L because L is not a durability
boundary; the error must state this candidly. Tests use injectable failpoints at
operation boundaries to exercise this generic recovery path.

## 16. Concurrency and scaling

One notebook has one ordered accepted state. Therefore, the `current` object is
the final serialization point.

Merge work, pack creation, uploads, and downloads happen concurrently. The CAS
request is the only serialized publication action.

The target planning workload is:

```text
100 agents
one commit per agent per minute
approximately 1 kB of new note content per commit
```

This equals approximately 1.67 accepted commits per second. V1 must benchmark
both evenly distributed commits and synchronized bursts.

High throughput depends on note layout. Changes to separate files usually
merge without caller action. Frequent overlapping changes to one file create
semantic conflicts that storage cannot remove.

Recommended note layouts use many focused files instead of one global append
file. Slivingdoc does not enforce agent namespaces.

Important performance measures include:

- accepted commits per second
- CAS attempts per accepted commit
- merge duration
- pack creation, upload, download, and import duration
- active tail pack count and bytes
- checkpoint duration and size
- cold and warm pull duration
- conflict count
- orphan and cleanup counts

V1 does not promise a fixed throughput before benchmarks run against the
intended S3 service. The architecture avoids known quadratic full-history
uploads and long writer critical sections.

## 17. Configuration

The process is a subcommand CLI. The subcommand precedes every flag.

| Command         | Effect                                                       |
| --------------- | ------------------------------------------------------------ |
| `serve` (`s`)   | Serve the two MCP tools over stdio.                           |
| `pull` (`p`)    | Write the current notebook into one directory, print the candid result, and exit. |
| `commit` (`c`)  | Publish the changes at one directory with `-m <message>`, print the candid result, and exit. |
| `version` (`v`) | Write the version line and exit zero, resolving no configuration. |

A missing or unknown command writes the command listing and exits nonzero;
no server starts.

Flags and environment variables configure the `serve`, `pull`, and
`commit` commands identically. `commit` adds the required `-m`/`--message`
flag. The two subcommands take exactly one positional notebook path, which
can follow or precede the flags. Flags take precedence over environment
variables. The application package owns exact parsing.

`pull` and `commit` run the same startup sequence as `serve`: the pinned
engine check and the S3 compatibility probe. They perform one operation and
exit. An argument refusal (a missing path or message) exits nonzero before
any native or network dependency is touched.

| Function              | Flag                     | Environment variable              |
| --------------------- | ------------------------ | --------------------------------- |
| S3 bucket             | `--bucket`               | `SLIVINGDOC_BUCKET`               |
| S3 prefix             | `--prefix`               | `SLIVINGDOC_PREFIX`               |
| S3 region             | `--region`               | `AWS_REGION`                      |
| S3 endpoint           | `--endpoint`             | `AWS_ENDPOINT_URL_S3`             |
| S3 path-style access  | `--path-style`           | `SLIVINGDOC_PATH_STYLE`           |
| Workspace root        | `--workspace-root`       | `SLIVINGDOC_WORKSPACE_ROOT`       |
| Private root          | `--private-root`         | `SLIVINGDOC_PRIVATE_ROOT`         |
| CAS retry limit       | `--commit-retries`       | `SLIVINGDOC_COMMIT_RETRIES`       |
| Checkpoint pack count | `--checkpoint-packs`     | `SLIVINGDOC_CHECKPOINT_PACKS`     |
| Retained generations  | `--retained-checkpoints` | `SLIVINGDOC_RETAINED_CHECKPOINTS` |

The bucket is required. The default prefix is `slivingdoc`, and the default
region is `us-east-1`. The endpoint is empty for normal AWS resolution.
Path-style access defaults to false.

A custom endpoint is an absolute `http` or `https` URL. It contains no user
information, query, or fragment. Configuration lowercases its scheme and host,
removes a trailing slash, and preserves a non-root path. This normalized value
is part of the P storage identity.

The workspace root defaults to the startup working directory. The private root
defaults to `<user-cache-dir>/slivingdoc`. Both roots become absolute before
startup. They cannot overlap, and the private root cannot be below the
workspace root.

The commit retry value counts retries after the first CAS attempt. Its default
is 8, and its valid range is 0 through 100. Retry delay uses full jitter from an
exponential ceiling. The first ceiling is 25 ms, and the maximum is 2 seconds.
The checkpoint pack-count default is 1,024 and its minimum is 1. Retained
checkpoint generations default to 1 and have a valid range of 0 through 64.

Flags override environment variables, which override defaults. An explicitly
empty flag value does not fall back to an environment value. Boolean values use
Go `strconv.ParseBool`. Decimal integer values do not accept a sign.

Logs go to stderr as structured `key=value` text. Every record carries a
timestamp, a level, and the module that emitted it. Stdout carries only MCP
protocol messages and command output.

The environment configures logging, so it applies to every command and is
resolved before flags are parsed. `LOG_LEVEL` is a comma-separated list in
which `module=level` sets one module's level and a bare `level` sets the
default, for example `cli=warn,mcp=debug,info`. The modules are `cli`,
`app`, `mcp`, and `notebook`; the levels are debug, info, warn, and error.
A malformed value is reported and falls back to the info default rather
than refusing startup. `NO_COLOR` set to any non-empty value disables the
level color.
SIGINT and SIGTERM stop new requests and cancel in-flight request contexts.
Shutdown waits at most 30 seconds, closes native and lock resources, and exits
nonzero if the deadline expires.

`slivingdoc version` writes `slivingdoc <semver>` and one LF to stdout, then
exits zero without loading configuration or native and S3 dependencies.
`slivingdoc serve -h`, `slivingdoc pull -h`, and `slivingdoc commit -h`
follow the same startup rule and write flag help to stdout. Invalid
configuration writes one redacted diagnostic to stderr and exits nonzero.

## 18. Transport and security

### 18.1 Stdio

An MCP host starts the server as a local child process. The host and server can
access the same visible directory.

The npm package is the primary stdio installation path:

```json
{
  "command": "npx",
  "args": ["-y", "slivingdoc", "serve"]
}
```

### 18.2 Path security

The service applies the same path rules:

- canonicalize the requested path
- require it to stay below the configured workspace root
- reject symlink traversal in every existing path component
- reject special files during scans
- avoid following links during replacement and cleanup
- use private directories that callers cannot select

S3 credentials and private repository data remain inside the server process.

## 19. Internal package boundaries

The expected repository shape is:

```text
main.go
internal/
  app/          configuration, dependency construction, startup, shutdown
  mcp/          tool schemas, transport, error mapping
  notebook/     pull, commit, conflict continuation, retries, checkpoints
  workspace/    path policy, visible-file snapshots, private path state
  git/          Go-facing Git engine contract and orchestration
  git2/         the only CGo and libgit2 boundary
  storage/      manifest model and semantic object-store contract
  s3store/      AWS SDK implementation and compatibility probe
npm/
  slivingdoc/   artifact selection, verification, and execution
```

Packages own invariants or system boundaries. They do not exist only to rename
or forward functions.

All Go packages are internal. The MCP tools, the pull and commit
subcommands, process flags, release artifacts, and npm launcher are the
supported interfaces.

## 20. Test architecture

The implementation must be testable without live AWS resources.

External effects use narrow seams:

| Effect              | Test seam                                                           |
| ------------------- | ------------------------------------------------------------------- |
| S3 operations       | Semantic object-store interface with a deterministic in-memory fake |
| Git behavior        | Go-facing engine interface for notebook orchestration tests         |
| libgit2             | Real native component tests in temporary directories                |
| Filesystem          | Temporary roots and controlled test fixtures                        |
| Time and backoff    | Injected wait function or deterministic retry policy                |
| Publication IDs     | Injected ID source                                                  |
| MCP                 | In-memory SDK transport and real process tests                      |
| npm download        | Local HTTP server and fixture artifacts                             |

The fake object store must implement conditional-write semantics, not only
successful reads and writes. Race tests use barriers so CAS winners and losers
are deterministic.

Integration tests use
[testcontainers-go](https://golang.testcontainers.org/) to start
[MinIO](https://min.io/). They prove real request encoding, ETag handling,
conditional writes, single and multipart pack transfer, checksum metadata,
endpoint configuration, concurrent publication, and read-after-write recovery.

MinIO is the required S3-compatible integration target. Deterministic adapter
tests emulate timeout-after-acceptance and other failures that MinIO cannot
reliably induce. No correctness path uses S3 `LIST` to discover accepted state.
Tests do not require a live AWS account.

Boundary tests include:

```text
libgit2 boundary    object, pack, shallow checkpoint, and merge conformance
storage boundary    fake contract suite and MinIO contract suite
notebook boundary   pull, commit, retry, conflict, checkpoint, and cleanup
MCP boundary        schemas, OK results, structured errors, and stdio
release boundary    native artifact startup and dependency inspection
npm boundary        platform selection, checksum, streams, and exit status
```

Docker-backed tests fail with an actionable diagnostic when Docker is
unavailable. They never skip, so a stopped local daemon cannot hide a
regression. CI runs them in a required job.

## 21. Distribution and native builds

The npm package contains a small Node launcher. It selects an artifact by npm
version, operating system, and architecture. It verifies the published SHA-256
checksum before execution.

Release tags use `v<semver>`. Assets use
`slivingdoc-v<semver>-<os>-<arch>` and add `.exe` on Windows. OS values are
`linux`, `darwin`, and `windows`. Architecture values are `amd64` and `arm64`.
The release contains one `SHA256SUMS` file with lowercase SHA-256, two spaces,
and the asset name on each LF-terminated line. Entries are sorted by asset name.

The launcher gets the exact version from its `package.json`. It downloads the
matching raw binary and `SHA256SUMS` from the same GitHub release tag. It writes
a unique temporary file, verifies the checksum, sets executable permission
where required, and atomically renames the file into the npm cache. The cache
key contains package version, OS, architecture, and asset name. The launcher
never executes an unverified or partial file.

The release executable includes the pinned libgit2 library. It must not require
`libgit2.so`, `libgit2.dylib`, `git2.dll`, or a Git executable on the target
machine.

Dependency inspection checks the complete runtime dependency list. Linux
binaries are fully static — built with musl-gcc and `-extldflags "-static"` —
so they carry no dynamic dependency and run on both glibc and musl (alpine).
macOS can use libraries in `/usr/lib` and `/System/Library`. Windows can use
documented Windows system DLLs. All other native dependencies must be linked
into the artifact or cause the target job to fail.

Native CGo builds run on suitable Linux, macOS, and Windows runners. The
reusable `baalimago/simple-go-pipeline` release workflow builds each target,
checks its runtime dependencies, smoke-tests the binary, and assembles the
release with its checksum file and notice.

The first supported targets are:

- Linux amd64 and arm64
- macOS amd64 and arm64
- Windows amd64

Windows arm64 can be added after its native toolchain and runner are proven.

One final release job creates the GitHub release after all target builds pass.
The npm publish step runs only after every required artifact and checksum is
available. It runs in the same workflow, after the release assembly, and
fails unless `npm/slivingdoc/package.json` reports the tag version. The
repository provides `make release`, an interactive script. The script
prints the recent releases, prompts for the new version and a tag
description, bumps the package version, commits it, and annotates the
`v<semver>` tag. Thus the workflow and the package cannot drift. A
prerelease version publishes to the `next` dist-tag. A stable version
publishes to `latest`. Publication uses npm trusted publishing (OIDC). The
repository stores no npm token, and the registry accepts publishes only
from the `release.yml` workflow of `baalimago/slivingdoc`. Provenance
attestations are generated automatically for the public repository.

## 22. Operational responsibility

slivingdoc guarantees:

- accepted state is indexed by one authoritative manifest
- a failed concurrent publication cannot silently overwrite accepted state
- accepted packs are immutable
- a successful commit is durably referenced by `current`
- merge conflicts do not advance remote state
- checkpoint and cleanup failures do not corrupt current state

Deployment owners decide:

- bucket versioning
- noncurrent-version retention
- replication
- object lock
- backup export
- recovery procedures
- storage lifecycle policy

These S3 features complement slivingdoc. They are not hidden prerequisites for
the synchronization algorithm.

## 23. Deferred work

The following work is outside v1:

- byte-based checkpoint thresholds
- multi-level pack compaction
- multiple independently ordered notebook partitions
- permanent or configurable logical history
- a public backup and restore API
- an administrative status API
- remote file-editing tools
- Windows arm64 artifacts
- a public Go SDK

The versioned manifest permits storage-format evolution. MCP callers do not
depend on the internal representation.

## 24. Decisions recorded

1. The MCP server is the product and the only programmatic protocol.
2. The server exposes `notes_pull(path)` and `notes_commit(path, message)`.
3. Users do not install Git or libgit2.
4. A narrow internal CGo package calls a pinned libgit2 release.
5. The AWS Go SDK performs all S3 operations.
6. S3 stores immutable incremental Git packs and one `current` manifest.
7. `current` indexes the active checkpoint and ordered incremental tail.
8. Publication uses ETag CAS and randomized bounded retries.
9. The design has no S3 writer lock.
10. The notebook supports UTF-8 text files without U+0000 only.
11. Libgit2 performs three-tree text merges.
12. Binary files are rejected before merge or publication.
13. Checkpointing starts automatically at a configurable pack count.
14. The default checkpoint threshold is 1,024 increments.
15. A checkpoint can compact a stable prefix while newer commits continue.
16. Checkpoints preserve current state, not permanent Git history.
17. The current and previous checkpoint generations are retained by default.
18. A subsequent checkpoint cleans older physical storage on a best-effort basis.
19. S3 versioning and backups are deployment policies.
20. V1 targets high agent concurrency and avoids full-history upload per commit.
21. External resources have mockable semantic boundaries.
22. MinIO testcontainers prove the real S3 integration contract.
23. The npm launcher downloads and verifies native GitHub release artifacts.
24. L is caller-controlled, P is private local state, and R is accepted remote state.
25. Synchronization occurs at MCP operation boundaries without a file watcher.
26. Pull merges local changes onto R and can rewrite L with conflict markers.
27. The first pull uses the canonical empty tree as its baseline.
28. V1 accepts valid UTF-8 text without U+0000 and rejects binary files.
29. Commit rejects complete slivingdoc conflict-marker blocks.
30. Unprovable ambiguous publication returns failure and is not replayed.
31. Manifest version 1 is a strict normative JSON protocol.
32. Publication and checkpoint identifiers use UUIDv7.
33. Active and retained descriptors are publication receipts.
34. Cleanup uses the successful checkpoint cutoff as its candidate fence.
35. Retained generations contain complete reconstructable descriptor chains.
36. Unexpected partial local mutation uses one generic recovery path.
37. The `pull` and `commit` subcommands expose the same two operations to
    humans; they reuse the serve startup surface and print the envelope as a
    candid text report.

## 25. Architecture acceptance

This design is ready for implementation when the worklog preserves these
invariants:

- `current` is the only accepted-state authority.
- Packs are immutable and uploaded before the manifest references them.
- Conditional manifest replacement prevents lost updates.
- CAS contention causes merge and retry, not silent overwrite.
- Conflicts rewrite L with merge results and markers; unexpected partial local
  mutation enters the explicit recovery path and never returns `OK`.
- Checkpoints bound cold-start pack count without blocking writers.
- Cleanup never determines commit success.
- The executable has no runtime Git or libgit2 installation requirement.
- Unit tests can replace all network services with deterministic fakes.
- Required integration tests run against MinIO through testcontainers.

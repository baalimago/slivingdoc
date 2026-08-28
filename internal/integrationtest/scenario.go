package integrationtest

// Tool names of the public API (architecture section 2). The catalog
// entries reference exactly these two tools.
const (
	toolPull   = "notes_pull"
	toolCommit = "notes_commit"
)

// ToolCall is one public API call of one session.
type ToolCall struct {
	// Tool is "notes_pull" or "notes_commit".
	Tool string
	// Path is the absolute request path.
	Path string
	// Message is the notes_commit message.
	Message string
	// Expect is the envelope expectation of the call result.
	Expect CallExpectation
}

// CallExpectation is the tool-result envelope expectation. Exactly one of
// OK and ErrorCode must be set.
type CallExpectation struct {
	// OK requires the success envelope: one text item carrying the resolved
	// notebook path and a structured SuccessInfo whose code is OK.
	OK bool
	// Success pins the exact structured success envelope (generation,
	// totals, and the ordered per-file stat) when non-nil; nil asserts the
	// shape only. Meaningful only when OK is set.
	Success *SuccessExpectation
	// ErrorCode requires the error envelope with this stable category.
	ErrorCode string
	// Retryable asserts the exact retryable flag when non-nil.
	Retryable *bool
	// Files asserts the exact ordered conflict-file list of the
	// structured error (relative paths and one-based inclusive ranges).
	Files []FileExpectation
	// Recovery asserts the RECOVERY_FAILURE report when non-nil.
	Recovery *RecoveryExpectation
	// NoText forbids substrings anywhere in the result text or the
	// structured content.
	NoText []string
}

// FileExpectation is one structured error file: a relative normalized path
// and its one-based inclusive marker ranges.
type FileExpectation struct {
	Path   string
	Ranges []RangeExpectation
}

// RangeExpectation is one conflict-marker block, one-based and inclusive.
type RangeExpectation struct {
	Start int
	End   int
}

// SuccessExpectation is the structured success-envelope expectation: the
// accepted remote generation after the operation, the total counts, and
// the exact ordered per-file change stat.
type SuccessExpectation struct {
	Generation   uint64
	FilesChanged int
	Insertions   int
	Deletions    int
	Files        []FileStatExpectation
}

// FileStatExpectation is one structured success file: the normalized
// relative path and its exact insertion and deletion counts.
type FileStatExpectation struct {
	Path       string
	Insertions int
	Deletions  int
}

// RecoveryExpectation is the RECOVERY_FAILURE report expectation: the
// failed stage, the remote-acceptance statement, and whether
// resynchronization succeeded (asserted only when Resynchronized is
// non-nil).
type RecoveryExpectation struct {
	Stage          string
	RemoteAccepted string
	Resynchronized *bool
}

// Expectations are the state assertions after the entry script settles.
// Asynchronous effects (checkpoint, cleanup) settle through polling; every
// assertion waits until the state is stable.
type Expectations struct {
	// FS is the visible-directory state; keyed by absolute host path.
	FS FSAssertions
	// S3 is the object-store state behind the harness store seam.
	S3 S3Assertions
	// Logs is the harness log-capture expectation.
	Logs *LogExpectations
}

// FSAssertions describe the visible-directory state after the run.
type FSAssertions struct {
	// Files maps an absolute host path to its exact expected bytes.
	Files map[string]string
	// Contains maps an absolute host path to a substring its bytes must
	// contain (conflict markers and similar).
	Contains map[string]string
	// Missing lists absolute host paths that must not exist.
	Missing []string
}

// S3Assertions describe the object-store state after the run, read through
// the raw store so injected faults never poison the assertions themselves.
type S3Assertions struct {
	// Counts asserts exact operation counters of the recorder seam.
	Counts *CountExpectation
	// NoCurrent requires the current manifest object to be absent when
	// non-nil.
	NoCurrent *bool
	// NoPacks requires every pack namespace to be empty when non-nil.
	NoPacks *bool
}

// CountExpectation asserts exact recorder operation counters. AllZero
// requires every operation count to be zero; Ops asserts the listed
// operations exactly (absent operations are not checked).
type CountExpectation struct {
	Ops     map[Op]int
	AllZero bool
}

// LogExpectations assert the harness log capture.
type LogExpectations struct {
	// WarnContains requires every substring to appear in some warning
	// record (cleanup-failure observability).
	WarnContains []string
	// NoSubstring forbids substrings anywhere in the capture (redaction).
	NoSubstring []string
}

package mcp

import "github.com/baalimago/slivingdoc/internal/notebook"

// SuccessInfo is the structured success object carried in the MCP tool
// result (architecture/product-contract.md). Code is always "OK";
// generation is the accepted remote generation after the operation;
// filesChanged, insertions, and deletions are the totals of the per-file
// change stat; files is always present, empty for a no-op synchronization.
// All paths are the same normalized internal slash form used by error
// files.
type SuccessInfo struct {
	Code string `json:"code"`
	// Path is the resolved notebook directory the operation ran against.
	// It is the caller's own visible directory, never private state.
	Path       string `json:"path"`
	Generation uint64 `json:"generation"`
	// Materialized marks a first pull into an empty directory: files is empty and outline describes the notebook.
	Materialized bool         `json:"materialized"`
	FilesChanged int          `json:"filesChanged"`
	Insertions   int          `json:"insertions"`
	Deletions    int          `json:"deletions"`
	Files        []ChangeFile `json:"files"`
	ReadOnly     []string     `json:"readOnly"`
	// Writable is the normalized writable set; a non-empty set makes every
	// path it does not cover read-only (architecture/product-contract.md,
	// Read-only and writable paths). Always present, empty when nothing is
	// configured.
	Writable []string `json:"writable"`
	// Outline is a materialization's outline; absent from every other result.
	Outline *Outline `json:"outline,omitempty"`
}

// ChangeFile is the per-file line-change summary of a success: the
// normalized internal path and its insertion and deletion counts.
type ChangeFile struct {
	Path       string `json:"path"`
	Insertions int    `json:"insertions"`
	Deletions  int    `json:"deletions"`
}

// MapSuccess converts a notebook result into the structured success
// object. The generation and the diffstat totals map directly; files is
// always non-nil, empty for a no-op and for a materialization, which
// carries its outline instead. No redaction is needed: the shape
// carries no credential, S3 key, private path, or Git ID, and the diffstat
// paths are already the normalized internal relative form used by error
// files.
func MapSuccess(result notebook.Result, path string) *SuccessInfo {
	info := &SuccessInfo{
		Code:         "OK",
		Path:         path,
		Generation:   result.Generation,
		Materialized: len(result.Contents) > 0,
		FilesChanged: len(result.Stat.Files),
		Insertions:   result.Stat.Insertions,
		Deletions:    result.Stat.Deletions,
		Files:        []ChangeFile{},
		ReadOnly:     []string{},
		Writable:     []string{},
	}
	if info.Materialized {
		info.Outline = outline(result.Contents)
		return info
	}
	for _, f := range result.Stat.Files {
		info.Files = append(info.Files, ChangeFile{Path: f.Path, Insertions: f.Insertions, Deletions: f.Deletions})
	}
	return info
}

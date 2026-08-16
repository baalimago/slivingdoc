package mcp

import "github.com/baalimago/slivingdoc/internal/notebook"

// SuccessInfo is the structured success object carried in the MCP tool
// result (architecture section 2). Code is always "OK"; generation is the
// accepted remote generation after the operation; filesChanged,
// insertions, and deletions are the totals of the per-file change stat;
// files is always present, empty for a no-op synchronization. All paths
// are the same normalized internal slash form used by error files.
type SuccessInfo struct {
	Code         string       `json:"code"`
	Generation   uint64       `json:"generation"`
	FilesChanged int          `json:"filesChanged"`
	Insertions   int          `json:"insertions"`
	Deletions    int          `json:"deletions"`
	Files        []ChangeFile `json:"files"`
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
// always non-nil, empty for a no-op. No redaction is needed: the shape
// carries no credential, S3 key, private path, or Git ID, and the diffstat
// paths are already the normalized internal relative form used by error
// files.
func MapSuccess(result notebook.Result) *SuccessInfo {
	files := make([]ChangeFile, 0, len(result.Stat.Files))
	for _, f := range result.Stat.Files {
		files = append(files, ChangeFile{Path: f.Path, Insertions: f.Insertions, Deletions: f.Deletions})
	}
	return &SuccessInfo{
		Code:         "OK",
		Generation:   result.Generation,
		FilesChanged: len(result.Stat.Files),
		Insertions:   result.Stat.Insertions,
		Deletions:    result.Stat.Deletions,
		Files:        files,
	}
}

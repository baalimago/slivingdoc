package notebook

import (
	"context"
	"fmt"
	"sort"

	"github.com/baalimago/slivingdoc/internal/git"
)

// ChangeKind classifies one local change against the accepted state.
type ChangeKind string

const (
	ChangeAdded    ChangeKind = "added"
	ChangeModified ChangeKind = "modified"
	ChangeDeleted  ChangeKind = "deleted"
)

// Change is one file whose visible content differs from the accepted state,
// with its line counts.
type Change struct {
	Path       string
	Kind       ChangeKind
	Insertions int
	Deletions  int
}

// Status is the local view of a notebook: what the last accepted state is
// and what a commit would publish. It reads no remote state.
type Status struct {
	// Generation is the accepted remote generation; zero before the first pull.
	Generation uint64
	// Pulled reports whether the directory has been pulled, which a commit requires.
	Pulled bool
	// RecoveryRequired reports that the next pull or commit resynchronizes
	// the directory first. Changes is empty then: the directory is not compared.
	RecoveryRequired bool
	// Changes lists the local changes sorted by path.
	Changes []Change
}

// Status compares the visible directory with the accepted state.
func (n *Notebook) Status(ctx context.Context) (Status, error) {
	ctx, release, err := n.holdWorkspace(ctx)
	if err != nil {
		return Status{}, err
	}
	defer release()

	st := Status{
		Generation:       n.ws.Baseline().RemoteGeneration,
		Pulled:           n.ws.Pulled(),
		RecoveryRequired: n.ws.RecoveryRequired(),
	}
	if st.RecoveryRequired {
		return st, nil
	}
	local, err := n.ws.Snapshot(ctx)
	if err != nil {
		return Status{}, n.mapLocalError(err)
	}
	base, err := git.ReadSnapshot(n.ws.Repo(), n.ws.Baseline().Tree)
	if err != nil {
		return Status{}, storageIntegrity(ReasonEngineFailed, err, "read the accepted state for the status")
	}
	st.Changes = changesBetween(base, local)
	return st, nil
}

func changesBetween(base, cur git.Snapshot) []Change {
	inBase := make(map[string]struct{}, len(base.Files))
	for _, f := range base.Files {
		inBase[f.Path] = struct{}{}
	}
	inCur := make(map[string]struct{}, len(cur.Files))
	for _, f := range cur.Files {
		inCur[f.Path] = struct{}{}
	}
	counts := make(map[string]git.FileStat)
	for _, fs := range git.DiffSnapshots(base, cur).Files {
		counts[fs.Path] = fs
	}
	baseData := make(map[string]string, len(base.Files))
	for _, f := range base.Files {
		baseData[f.Path] = string(f.Data)
	}

	var out []Change
	for _, f := range cur.Files {
		_, existed := inBase[f.Path]
		switch {
		case !existed:
			out = append(out, Change{Path: f.Path, Kind: ChangeAdded, Insertions: counts[f.Path].Insertions})
		case baseData[f.Path] != string(f.Data):
			c := counts[f.Path]
			out = append(out, Change{Path: f.Path, Kind: ChangeModified, Insertions: c.Insertions, Deletions: c.Deletions})
		}
	}
	for _, f := range base.Files {
		if _, ok := inCur[f.Path]; !ok {
			out = append(out, Change{Path: f.Path, Kind: ChangeDeleted, Deletions: counts[f.Path].Deletions})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// LogEntry is one accepted publication.
type LogEntry struct {
	Message string
}

// History is the recent accepted publications, newest first.
type History struct {
	Entries []LogEntry
	// More reports that older publications exist beyond Entries, or were
	// dropped by a checkpoint.
	More bool
}

// Log returns up to limit accepted publications, newest first, from the
// history this machine holds. It reads no remote state, so a directory that
// was never pulled has none.
func (n *Notebook) Log(ctx context.Context, limit int) (History, error) {
	if limit < 1 {
		return History{}, invalidRequest(ReasonMalformedInput, nil, nil, "the log limit must be at least 1, got %d", limit)
	}
	_, release, err := n.holdWorkspace(ctx)
	if err != nil {
		return History{}, err
	}
	defer release()

	var h History
	repo := n.ws.Repo()
	id := n.ws.Baseline().Head
	for !id.IsZero() {
		if len(h.Entries) == limit {
			h.More = true
			return h, nil
		}
		commit, err := repo.ReadCommit(id)
		if err != nil {
			return History{}, storageIntegrity(ReasonEngineFailed, fmt.Errorf("read publication: %w", err), "read the history for the log")
		}
		h.Entries = append(h.Entries, LogEntry{Message: commit.Message})
		if len(commit.Parents) == 0 {
			return h, nil
		}
		parent := commit.Parents[0]
		present, err := repo.HasObject(parent)
		if err != nil {
			return History{}, storageIntegrity(ReasonEngineFailed, err, "read the history for the log")
		}
		if !present {
			h.More = true
			return h, nil
		}
		id = parent
	}
	return h, nil
}

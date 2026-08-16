package notebook

import "github.com/baalimago/slivingdoc/internal/git"

// Result is the success summary of one pull or commit: the accepted remote
// generation after the operation and the per-file line-change diffstat of
// what the operation changed. Pull reports the delta between the visible
// state it observed and the materialized result; commit reports the
// increment the publication added over the observed remote parent tree,
// empty for a no-op synchronization. The zero Result is always paired with
// a non-nil error; consumers read Result only when err is nil.
type Result struct {
	Generation uint64
	Stat       git.DiffStat
}

// diffStat reads the base and result trees of an accepted operation and
// computes their line diffstat. A snapshot-read failure maps to the
// storage-integrity path with a zero result: the summary is
// presentation-only, so the operation aborts before any local mutation or
// publication when the diff cannot be computed.
func (n *Notebook) diffStat(base, result git.OID) (git.DiffStat, error) {
	baseSnap, err := git.ReadSnapshot(n.ws.Repo(), base)
	if err != nil {
		return git.DiffStat{}, storageIntegrity(err, "read the base snapshot for the change summary")
	}
	resultSnap, err := git.ReadSnapshot(n.ws.Repo(), result)
	if err != nil {
		return git.DiffStat{}, storageIntegrity(err, "read the result snapshot for the change summary")
	}
	return git.DiffSnapshots(baseSnap, resultSnap), nil
}

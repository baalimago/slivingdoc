package notebook

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// Commit publishes the caller's changes and incorporates concurrent,
// non-conflicting changes (architecture section 11). It requires a
// non-blank message and a managed pull, validates every visible file and
// rejects complete conflict-marker blocks before any Git or S3 work, then
// merges the accepted baseline, L, and R. A clean result creates a commit
// whose parent is the observed R head (or none for the first publication),
// uploads one immutable pack before its manifest CAS, and rewrites L and P
// to the accepted state only after acceptance is proved. A CAS loss imports
// the new remote tail, merges again, and retries with a new publication ID,
// generation, key, commit, and pack up to the configured bound. A
// no-change result synchronizes L and P to R without any remote mutation.
//
// The returned Result reports the accepted generation and the diffstat of
// the published increment: the observed remote parent tree versus the
// accepted merged tree. A no-op commit returns the remote generation with
// an empty stat. A conflict or any error returns the zero Result with the
// existing error.
func (n *Notebook) Commit(ctx context.Context, message string) (Result, error) {
	if n.ws.RecoveryRequired() {
		if err := n.entryRecovery(ctx); err != nil {
			return Result{}, err
		}
	}
	if err := ValidateMessage(message); err != nil {
		return Result{}, err
	}
	if !n.ws.Pulled() {
		return Result{}, invalidRequest(ReasonPullRequired, nil, nil, "commit requires a successful or conflicting pull first")
	}

	local, err := n.ws.Snapshot(ctx)
	if err != nil {
		return Result{}, n.mapLocalError(err)
	}
	if files := rejectMarkers(local); len(files) > 0 {
		return Result{}, contentConflict(ReasonUnresolvedMarkers, "Resolve the conflict blocks before notes_commit.", files)
	}
	localTree, err := git.BuildTree(n.ws.Repo(), local)
	if err != nil {
		return Result{}, invalidRequest(ReasonInvalidContent, err, nil, "visible files cannot be represented as notebook state")
	}
	if err := n.enforcePolicy(ctx, local, localTree); err != nil {
		return Result{}, err
	}

	baseTree := n.ws.Baseline().Tree
	attemptStart := n.now()

	for attempt := 1; ; attempt++ {
		casLost, result, err := n.attemptPublication(ctx, message, baseTree, localTree, attemptStart)
		if err != nil {
			return Result{}, err
		}
		if !casLost {
			return result, nil
		}
		if attempt > n.retryLimit {
			return Result{}, remoteBusy("another writer kept winning the publication race for %d attempts", attempt)
		}
		if err := n.waiter.Wait(ctx, attempt); err != nil {
			return Result{}, err
		}
	}
}

// attemptPublication runs one complete publication attempt against a fresh
// remote observation: read, merge, build, upload, CAS, local acceptance,
// opportunistic checkpoint. casLost is true only when the manifest CAS lost
// the race and the caller owns the bounded retry; every other outcome is
// terminal. result carries the winning attempt's change summary and is
// meaningful only when casLost is false and err is nil; a lost attempt's
// summary is discarded with the attempt.
func (n *Notebook) attemptPublication(ctx context.Context, message string, baseTree, localTree git.OID, attemptStart time.Time) (casLost bool, result Result, err error) {
	remote, err := n.readRemote(ctx)
	if err != nil {
		return false, Result{}, err
	}

	merged, err := git.Merge(n.ws.Repo(), baseTree, localTree, remote.tree)
	if err != nil {
		return false, Result{}, storageIntegrity(ReasonEngineFailed, err, "merge failed")
	}
	if len(merged.Conflicts) > 0 {
		tree, err := n.materializeTree(merged)
		if err != nil {
			return false, Result{}, storageIntegrity(ReasonEngineFailed, err, "materialize conflict result")
		}
		if err := n.applyLocal(ctx, stageConflict, RemoteAcceptedNo, func() error {
			return n.ws.Materialize(ctx, remote.baseline(), tree)
		}); err != nil {
			return false, Result{}, err
		}
		return false, Result{}, contentConflict(ReasonMergeConflict, "Resolve the conflict blocks before notes_commit.",
			contentConflictFiles(merged.Conflicts))
	}

	if merged.Tree == remote.tree {
		// No local change survives the merge: synchronize L and P to R
		// without a publication ID, commit, pack, or CAS request.
		return false, Result{Generation: remote.generation}, n.applyLocal(ctx, stageCommit, RemoteAcceptedNo, func() error {
			return n.ws.Accept(ctx, remote.baseline())
		})
	}

	// The change summary is presentation-only: compute it against the
	// observed remote tree before any publication, so a read failure
	// aborts with no remote or local mutation.
	stat, err := n.diffStat(remote.tree, merged.Tree)
	if err != nil {
		return false, Result{}, err
	}

	proposal, err := n.buildProposal(ctx, remote, merged.Tree, message, attemptStart)
	if err != nil {
		return false, Result{}, err
	}
	if err := n.uploadProposal(ctx, proposal); err != nil {
		// A full space refuses the increment. When the whole state as one
		// checkpoint is smaller than what the space holds now, publish the
		// commit as that checkpoint instead, so deleting notes frees room.
		if !errors.Is(err, storage.ErrQuotaExceeded) || proposal.checkpointID != (storage.UUID{}) {
			return false, Result{}, err
		}
		compacted, ok, cerr := n.buildCompactingProposal(remote, proposal)
		if cerr != nil {
			return false, Result{}, cerr
		}
		if !ok {
			return false, Result{}, err
		}
		if err := n.uploadProposal(ctx, compacted); err != nil {
			return false, Result{}, err
		}
		proposal = compacted
	}

	// The manifest CAS is the atomic acceptance action. Uploading the
	// pack alone is only a proposal.
	err = n.publish(ctx, remote, proposal)
	if proposal.compacting && err != nil {
		n.discardCompaction(ctx, proposal, err)
	}
	if errors.Is(err, errCASLost) {
		return true, Result{}, nil
	}
	if err != nil {
		return false, Result{}, err
	}

	if fp := n.failpoints; fp != nil && fp.CAS != nil {
		if err := fp.CAS(); err != nil {
			return false, Result{}, n.failAfterAccept(ctx, stageCAS, err)
		}
	}

	if err := n.applyLocal(ctx, stageCommit, RemoteAcceptedYes, func() error {
		return n.ws.Accept(ctx, proposal.baseline)
	}); err != nil {
		return false, Result{}, err
	}

	// The commit is accepted. Checkpoint scheduling is opportunistic:
	// when the accepted tail reached the threshold, run one bounded
	// effort whose failure never changes this OK result (architecture
	// section 13.1).
	n.recordTail(proposal.manifest)
	if proposal.compacting {
		n.acceptCompaction(ctx, proposal)
	}
	if len(proposal.manifest.Increments) >= n.checkpointPacks {
		n.runCheckpoint(ctx, proposal.manifest)
	}
	return false, Result{Generation: proposal.baseline.RemoteGeneration, Stat: stat}, nil
}

// uploadProposal writes the proposal's immutable pack to its unique key
// with the descriptor metadata. The pack alone never publishes anything.
func (n *Notebook) uploadProposal(ctx context.Context, p proposal) error {
	meta := storage.Metadata{
		SHA256:     p.pack.SHA256,
		Size:       uint64(len(p.pack.Data)),
		Kind:       p.key.Kind,
		Generation: p.key.Generation,
	}
	if err := storage.UploadUnique(ctx, n.store, p.key, bytes.NewReader(p.pack.Data), meta); err != nil {
		return n.mapUploadError(err)
	}
	return nil
}

// proposal is one complete publication attempt: unique IDs, one immutable
// pack, the manifest that references it, and the accepted baseline it
// creates. Every retry rebuilds the whole proposal against the newly
// observed manifest; a losing attempt's pack is never republished at a
// later generation.
type proposal struct {
	publicationID storage.UUID
	checkpointID  storage.UUID
	key           storage.Key
	pack          git.Pack
	manifest      storage.Manifest
	baseline      workspace.Baseline
	// compacting marks a commit published as a checkpoint that replaces
	// the whole active chain because the space was full.
	compacting bool
}

// buildProposal builds one attempt against the observed remote state: a
// root commit and state-complete checkpoint pack for the first publication,
// or a normal commit and one incremental pack otherwise. It never mutates
// remote state.
func (n *Notebook) buildProposal(ctx context.Context, remote remoteState, mergedTree git.OID, message string, attemptStart time.Time) (proposal, error) {
	pubID, err := n.newID()
	if err != nil {
		return proposal{}, storageFailure(ReasonInternal, err, "generate publication id")
	}
	if remote.generation == 0 {
		return n.buildFirstProposal(ctx, remote, mergedTree, message, attemptStart, pubID)
	}
	return n.buildIncrementProposal(ctx, remote, mergedTree, message, attemptStart, pubID)
}

// buildFirstProposal creates the root commit, the state-complete
// checkpoint pack, the shallow boundary, and the generation-1 manifest with
// an empty incremental tail (architecture section 11.1).
func (n *Notebook) buildFirstProposal(ctx context.Context, remote remoteState, mergedTree git.OID, message string, attemptStart time.Time, pubID storage.UUID) (proposal, error) {
	head, err := git.CreateCommit(n.ws.Repo(), git.CommitSpec{Message: message, Tree: mergedTree, Time: attemptStart})
	if err != nil {
		return proposal{}, storageIntegrity(ReasonEngineFailed, err, "create the first commit")
	}
	pack, err := git.ExportCheckpoint(n.ws.Repo(), head)
	if err != nil {
		return proposal{}, storageIntegrity(ReasonEngineFailed, err, "could not prepare the full notebook state for publication")
	}
	if err := git.MarkShallow(n.ws.Repo(), head); err != nil {
		return proposal{}, storageIntegrity(ReasonEngineFailed, err, "record the checkpoint boundary")
	}
	cpID, err := n.newID()
	if err != nil {
		return proposal{}, storageFailure(ReasonInternal, err, "generate checkpoint id")
	}
	key := storage.Key{Kind: storage.KindCheckpoint, Generation: 1, ID: cpID}
	return proposal{
		publicationID: pubID,
		checkpointID:  cpID,
		key:           key,
		pack:          pack,
		manifest: storage.Manifest{
			Version:    1,
			Generation: 1,
			Head:       head,
			Checkpoint: storage.Checkpoint{
				ID:                cpID,
				Publication:       pubID,
				ThroughGeneration: 1,
				Head:              head,
				Key:               key,
				SHA256:            pack.SHA256,
				Size:              uint64(len(pack.Data)),
			},
			Increments: []storage.Increment{},
			Retained:   []storage.Retained{},
		},
		baseline: workspace.Baseline{RemoteGeneration: 1, Head: head, Tree: mergedTree},
	}, nil
}

// buildIncrementProposal creates a normal commit with the observed R head
// as its single parent and one incremental pack against that exact base.
func (n *Notebook) buildIncrementProposal(ctx context.Context, remote remoteState, mergedTree git.OID, message string, attemptStart time.Time, pubID storage.UUID) (proposal, error) {
	head, err := git.CreateCommit(n.ws.Repo(), git.CommitSpec{
		Message: message,
		Tree:    mergedTree,
		Parents: []git.OID{remote.head},
		Time:    attemptStart,
	})
	if err != nil {
		return proposal{}, storageIntegrity(ReasonEngineFailed, err, "create commit")
	}
	pack, err := git.ExportIncrement(n.ws.Repo(), head, remote.head)
	if err != nil {
		return proposal{}, storageIntegrity(ReasonEngineFailed, err, "could not prepare changed files for publication")
	}
	// The increment's target generation continues the active increment
	// chain from the checkpoint cutoff. The manifest generation counter
	// also advances on every checkpoint replacement, so it is not the
	// chain position (architecture sections 9.2 and 13.3): after a
	// checkpoint through generation N with a tail of M increments, the
	// next increment is generation N+M+1.
	incGeneration := remote.manifest.Checkpoint.ThroughGeneration + uint64(len(remote.manifest.Increments)) + 1
	key := storage.Key{Kind: storage.KindIncrement, Generation: incGeneration, ID: pubID}
	manifest := remote.manifest
	manifest.Generation = remote.generation + 1
	manifest.Head = head
	manifest.Increments = append(append([]storage.Increment(nil), remote.manifest.Increments...), storage.Increment{
		Generation:  incGeneration,
		Publication: pubID,
		Parent:      remote.head,
		Head:        head,
		Key:         key,
		SHA256:      pack.SHA256,
		Size:        uint64(len(pack.Data)),
	})
	return proposal{
		publicationID: pubID,
		key:           key,
		pack:          pack,
		manifest:      manifest,
		baseline:      workspace.Baseline{RemoteGeneration: remote.generation + 1, Head: head, Tree: mergedTree},
	}, nil
}

// buildCompactingProposal turns a refused increment proposal into one that
// publishes the same commit as a checkpoint through the increment's
// generation, replacing the active checkpoint and its whole tail. The space
// is full, so no previous generation is retained: every older pack becomes
// a cleanup candidate once the manifest CAS accepts the proposal. ok is
// false when the checkpoint would not be smaller than the active chain it
// and its retained generations reference, because then compaction frees no
// space and would only move the account further past its limit.
func (n *Notebook) buildCompactingProposal(remote remoteState, inc proposal) (proposal, bool, error) {
	pack, err := git.ExportCheckpoint(n.ws.Repo(), inc.manifest.Head)
	if err != nil {
		return proposal{}, false, storageIntegrity(ReasonEngineFailed, err, "could not prepare the full notebook state for publication")
	}
	// Everything the manifest references is freed by the cleanup that
	// follows acceptance, so it is the floor of what the space holds now.
	referenced := chainSize(remote.manifest.Checkpoint, remote.manifest.Increments)
	for _, r := range remote.manifest.Retained {
		referenced += chainSize(r.Checkpoint, r.Increments)
	}
	if uint64(len(pack.Data)) >= referenced {
		return proposal{}, false, nil
	}
	cpID, err := n.newID()
	if err != nil {
		return proposal{}, false, storageFailure(ReasonInternal, err, "generate checkpoint id")
	}
	key := storage.Key{Kind: storage.KindCheckpoint, Generation: inc.key.Generation, ID: cpID}
	manifest := inc.manifest
	manifest.Checkpoint = storage.Checkpoint{
		ID:                cpID,
		Publication:       inc.publicationID,
		ThroughGeneration: inc.key.Generation,
		Head:              inc.manifest.Head,
		Key:               key,
		SHA256:            pack.SHA256,
		Size:              uint64(len(pack.Data)),
	}
	manifest.Increments = []storage.Increment{}
	manifest.Retained = []storage.Retained{}
	return proposal{
		publicationID: inc.publicationID,
		checkpointID:  cpID,
		key:           key,
		pack:          pack,
		manifest:      manifest,
		baseline:      inc.baseline,
		compacting:    true,
	}, true, nil
}

// acceptCompaction finishes an accepted compacting commit: it records the
// checkpoint boundary and removes the packs the manifest no longer
// references. Both are best effort, like an opportunistic checkpoint: a
// failure is logged and never changes the accepted result.
func (n *Notebook) acceptCompaction(ctx context.Context, p proposal) {
	logger := LoggerFrom(ctx)
	n.metrics.CheckpointRuns.Add(1)
	n.metrics.CheckpointSize.Store(uint64(len(p.pack.Data)))
	if err := git.MarkShallow(n.ws.Repo(), p.manifest.Head); err != nil {
		// The boundary is local; freeing the space does not depend on it.
		n.failCheckpoint(logger, "record checkpoint boundary", err)
	}
	n.cleanup(ctx, p.key.Generation)
}

// discardCompaction deletes the checkpoint pack of a compacting proposal
// the manifest definitely did not accept: a lost CAS, or a definite refusal
// of the manifest write. The space is full, so an orphan that large could
// leave no room for the next compaction. Every other error may hide an
// accepted manifest that references the pack (a lost response whose
// follow-up read also failed), so the pack stays for a later cleanup.
// Deletion is best effort.
func (n *Notebook) discardCompaction(ctx context.Context, p proposal, cause error) {
	var e *Error
	definite := errors.Is(cause, errCASLost) || (errors.As(cause, &e) && e.Reason == ReasonManifestWrite)
	if !definite {
		return
	}
	if err := n.store.DeleteObjects(ctx, []string{p.key.String()}); err != nil {
		n.failCleanup(LoggerFrom(ctx), "delete unaccepted compaction checkpoint", err)
	}
}

// chainSize sums the pack sizes of one checkpoint and its increments.
func chainSize(cp storage.Checkpoint, incs []storage.Increment) uint64 {
	size := cp.Size
	for _, i := range incs {
		size += i.Size
	}
	return size
}

// publish performs the conditional manifest update: If-None-Match creation
// for the first publication, exact-ETag replacement otherwise. A
// precondition failure returns errCASLost; a lost response is resolved by
// reading current and searching for the proposal's publication ID
// (architecture section 11.3). Success is returned only when acceptance is
// proved.
func (n *Notebook) publish(ctx context.Context, remote remoteState, p proposal) error {
	manifestBytes, err := storage.EncodeManifest(p.manifest)
	if err != nil {
		return storageIntegrity(ReasonEngineFailed, err, "encode proposal manifest")
	}
	if remote.generation == 0 {
		_, err = n.store.CreateObject(ctx, storage.CurrentKey, manifestBytes)
	} else {
		_, err = n.store.ReplaceObject(ctx, storage.CurrentKey, remote.etag, manifestBytes)
	}
	switch {
	case err == nil:
		return nil
	case errors.Is(err, storage.ErrPreconditionFailed):
		return errCASLost
	case errors.Is(err, storage.ErrTransport):
		// The response was lost after the request was sent: read current
		// and search for the publication ID before deciding the result.
		found, lerr := n.lookupPublication(ctx, p.publicationID)
		if lerr != nil {
			return lerr
		}
		if found {
			return nil
		}
		return storageFailure(ReasonPublicationUnproven, err, "manifest acceptance cannot be proved; the proposal is not republished")
	default:
		return storageFailure(ReasonManifestWrite, err, "manifest CAS failed")
	}
}

// mapUploadError maps a failed pack upload: the pack is a proposal only, so
// the remote accepted state is unchanged. A unique-key collision is an
// integrity failure; every other upload failure is a storage failure.
func (n *Notebook) mapUploadError(err error) error {
	if errors.Is(err, storage.ErrIntegrity) {
		return storageIntegrity(ReasonPackInvalid, err, "pack upload collided with different bytes")
	}
	return storageFailure(ReasonPackUpload, err, "pack upload failed")
}

// enforcePolicy refuses a commit that changed a protected path, resetting
// exactly those files to the baseline through applyLocal (architecture
// section 11.1, Read-only paths). localTree is the tree the commit already
// built, so detection builds none of its own.
func (n *Notebook) enforcePolicy(ctx context.Context, local git.Snapshot, localTree git.OID) error {
	if !n.policy.Configured() {
		return nil
	}
	baseline := n.ws.Baseline()
	changed, err := n.policy.ChangedProtected(n.ws.Repo(), localTree, baseline.Tree)
	if err != nil {
		return storageIntegrity(ReasonEngineFailed, err, "read the baseline snapshot for the read-only check")
	}
	if len(changed) == 0 {
		return nil
	}
	restored, err := n.restoreProtected(baseline.Tree, local, changed)
	if err != nil {
		return err
	}
	tree, err := git.BuildTree(n.ws.Repo(), restored)
	if err != nil {
		return invalidRequest(ReasonInvalidContent, err, nil, "visible files cannot be represented as notebook state")
	}
	if err := n.applyLocal(ctx, stageReadOnly, RemoteAcceptedNo, func() error {
		return n.ws.Materialize(ctx, baseline, tree)
	}); err != nil {
		return err
	}
	return n.policyRefusal(changed)
}

// restoreProtected reads the baseline content of the changed paths and maps
// the two failures the policy can return: a repository read failure is an
// engine failure, while a restored snapshot the notebook cannot represent is
// caller-visible invalid content. The reader records the read failure, since
// only the boundary it crossed separates the two.
func (n *Notebook) restoreProtected(baseTree git.OID, local git.Snapshot, changed []string) (git.Snapshot, error) {
	reader := &readErrorRepo{Repository: n.ws.Repo()}
	restored, err := n.policy.RestoreProtected(reader, baseTree, local, changed)
	switch {
	case err == nil:
		return restored, nil
	case reader.err != nil:
		return git.Snapshot{}, storageIntegrity(ReasonEngineFailed, err, "read the baseline snapshot for the read-only check")
	default:
		return git.Snapshot{}, invalidRequest(ReasonInvalidContent, err, nil, "visible files cannot be represented as notebook state")
	}
}

// readErrorRepo records the first read failure of the wrapped repository.
type readErrorRepo struct {
	git.Repository
	err error
}

func (r *readErrorRepo) ReadTree(id git.OID) ([]git.TreeEntry, error) {
	entries, err := r.Repository.ReadTree(id)
	r.record(err)
	return entries, err
}

func (r *readErrorRepo) ReadBlob(id git.OID) ([]byte, error) {
	data, err := r.Repository.ReadBlob(id)
	r.record(err)
	return data, err
}

func (r *readErrorRepo) record(err error) {
	if err != nil && r.err == nil {
		r.err = err
	}
}

// policyRefusal builds the READ_ONLY_PATH error: one file entry per changed
// path, and a message naming where the caller may write.
func (n *Notebook) policyRefusal(changed []string) error {
	files := make([]ErrorFile, 0, len(changed))
	for _, path := range changed {
		files = append(files, ErrorFile{Path: path, Reason: FileReasonReadOnly})
	}
	return invalidRequest(ReasonReadOnlyPath, nil, files, "%s", n.refusalMessage(changed))
}

// refusalMessage names the writable entries when the operator declared any,
// since a default-protected policy protects nearly the whole notebook and
// only the writable set is a list the caller can act on (architecture
// section 2, Read-only paths). With no writable set the wording is the
// read-only one.
func (n *Notebook) refusalMessage(changed []string) string {
	if writable := n.policy.Writable(); len(writable) > 0 {
		return writableMessage(writable)
	}
	return readOnlyMessage(violatedEntries(n.readOnly, changed))
}

// writableMessage is the refusal text of a policy with a writable set.
func writableMessage(entries []string) string {
	verb := "is"
	if len(entries) > 1 {
		verb = "are"
	}
	return fmt.Sprintf("Only %s %s writable in this server. Your changes elsewhere were discarded and the files reset. Write under the writable paths, then commit again.",
		strings.Join(entries, ReadOnlyListSeparator), verb)
}

// violatedEntries returns, sorted and deduplicated, the most specific
// entry covering each changed path, since a set can hold an entry below
// another (architecture section 2, Writable paths).
func violatedEntries(set git.EntrySet, changed []string) []string {
	seen := make(map[string]bool, len(changed))
	var out []string
	for _, path := range changed {
		entry, ok := set.CoveringEntry(path)
		if !ok || seen[entry] {
			continue
		}
		seen[entry] = true
		out = append(out, entry)
	}
	sort.Strings(out)
	return out
}

// readOnlyMessage is the refusal text fixed by architecture section 2.
func readOnlyMessage(entries []string) string {
	verb := "is"
	if len(entries) > 1 {
		verb = "are"
	}
	return fmt.Sprintf("%s %s read-only in this server. Your changes there were discarded and the files reset. Write outside the read-only paths, then commit again.",
		strings.Join(entries, ReadOnlyListSeparator), verb)
}

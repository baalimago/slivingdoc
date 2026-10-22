package notebook

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/git"
	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// Cipher is the key material of one encryption command
// (worklogs/26-10-05-encrypted-notebooks/phase-4-command-surface.md). The raw
// store holds current and the packs exactly as stored; Open opens an existing
// encrypted current and may be nil for a plaintext notebook; Seal wraps the
// new pack and current, and Doors are written into the new encrypted current.
type Cipher struct {
	Store storage.ObjectStore
	Open  *crypt.Keyring
	Seal  *crypt.Keyring
	Doors []crypt.Door
}

// EncryptionStatus reports the stored encryption mode of one notebook.
// Enabled is false for an absent or version-one plaintext current. Locked is
// true for an encrypted current the caller asked to classify without a
// keyring; Doors and Source are set only for one this build opened.
type EncryptionStatus struct {
	Enabled bool
	Locked  bool
	Doors   []crypt.Door
	Source  crypt.CandidateSource
}

// EncryptStatus reads the raw current object and reports the notebook's
// encryption mode. A nil keyring classifies without unlocking; otherwise the
// keyring's candidates open the encrypted current, and a candidate that opens
// it records its ResolvedSource.
func (n *Notebook) EncryptStatus(ctx context.Context, raw storage.ObjectStore, keyring *crypt.Keyring) (EncryptionStatus, error) {
	data, _, present, err := readRawCurrent(ctx, raw)
	if err != nil {
		return EncryptionStatus{}, err
	}
	if !present {
		return EncryptionStatus{}, nil
	}
	encoding, envelope, err := storage.ClassifyCurrent(data)
	if err != nil {
		return EncryptionStatus{}, manifestError(err)
	}
	if encoding == storage.CurrentPlaintext {
		return EncryptionStatus{}, nil
	}
	if keyring == nil {
		return EncryptionStatus{Enabled: true, Locked: true}, nil
	}
	_, header, err := keyring.Open(ctx, storage.CurrentKey, envelope)
	if err != nil {
		return EncryptionStatus{}, encryptionOpenError(err)
	}
	return EncryptionStatus{Enabled: true, Doors: header.Doors, Source: keyring.ResolvedSource()}, nil
}

// EncryptDoors replaces the door set of the encrypted current object with
// doors. The payload ciphertext is untouched, so no pack byte changes; a lost
// conditional write re-reads the current object and retries within the
// notebook's bound. It returns the accepted manifest generation.
func (n *Notebook) EncryptDoors(ctx context.Context, raw storage.ObjectStore, keyring *crypt.Keyring, doors []crypt.Door) (uint64, error) {
	ctx, release, err := n.holdWorkspace(ctx)
	if err != nil {
		return 0, err
	}
	defer release()
	if n.ws.RecoveryRequired() {
		if err := n.entryRecovery(ctx); err != nil {
			return 0, err
		}
	}
	for attempt := 1; ; attempt++ {
		data, etag, present, err := readRawCurrent(ctx, raw)
		if err != nil {
			return 0, err
		}
		if !present {
			return 0, invalidRequest(ReasonEncryptionNotEnabled, nil, nil, "the notebook is not encrypted")
		}
		encoding, envelope, err := storage.ClassifyCurrent(data)
		if err != nil {
			return 0, manifestError(err)
		}
		if encoding == storage.CurrentPlaintext {
			return 0, invalidRequest(ReasonEncryptionNotEnabled, nil, nil, "the notebook is not encrypted")
		}
		plaintext, _, err := keyring.Open(ctx, storage.CurrentKey, envelope)
		if err != nil {
			return 0, encryptionOpenError(err)
		}
		manifest, err := storage.DecodeManifest(plaintext)
		if err != nil {
			return 0, manifestError(err)
		}
		rewritten, err := keyring.Rewrap(storage.CurrentKey, envelope, doors)
		if err != nil {
			return 0, storageIntegrity(ReasonEnvelopeInvalid, err, "rewrite the current door set")
		}
		wrapped, err := storage.WrapCurrentEnvelope(rewritten)
		if err != nil {
			return 0, storageIntegrity(ReasonEnvelopeInvalid, err, "encode the current envelope")
		}
		_, err = raw.ReplaceObject(ctx, storage.CurrentKey, etag, wrapped)
		switch {
		case err == nil:
			return manifest.Generation, nil
		case errors.Is(err, storage.ErrPreconditionFailed):
			if attempt > n.retryLimit {
				return 0, remoteBusy("another writer kept winning the door-set race for %d attempts", attempt)
			}
			if werr := n.waiter.Wait(ctx, attempt); werr != nil {
				return 0, werr
			}
		default:
			return 0, storageFailure(ReasonManifestWrite, err, "replace the current manifest")
		}
	}
}

// EncryptInit establishes encryption: it publishes the visible and accepted
// state as one encrypted root checkpoint under a new notebook key. A notebook
// whose stored current is already encrypted is refused before any write.
func (n *Notebook) EncryptInit(ctx context.Context, c Cipher, message string) (Result, error) {
	return n.publishRoot(ctx, c, message, false)
}

// EncryptRekey replaces the notebook key and publication history: it publishes
// the visible and accepted state as one root checkpoint under the new key. It
// requires an already encrypted notebook and refuses a plaintext one.
func (n *Notebook) EncryptRekey(ctx context.Context, c Cipher, message string) (Result, error) {
	return n.publishRoot(ctx, c, message, true)
}

// publishRoot publishes the merged visible and accepted state as one encrypted
// root checkpoint under c.Seal and c.Doors. rekey requires the stored current
// to be encrypted; init requires it to be absent or plaintext, reads the
// accepted state exactly as a commit does, and needs an established workspace
// baseline for a non-empty remote. Local and accepted changes merge exactly as
// a commit's; a conflict materializes markers and publishes nothing. Before
// publication the visible snapshot must be marker-free and must respect the
// configured path policy.
func (n *Notebook) publishRoot(ctx context.Context, c Cipher, message string, rekey bool) (Result, error) {
	ctx, release, err := n.holdWorkspace(ctx)
	if err != nil {
		return Result{}, err
	}
	defer release()
	if n.ws.RecoveryRequired() {
		if err := n.entryRecovery(ctx); err != nil {
			return Result{}, err
		}
	}
	if err := ValidateMessage(message); err != nil {
		return Result{}, err
	}
	local, err := n.ws.Snapshot(ctx)
	if err != nil {
		return Result{}, n.mapLocalError(err)
	}
	if files := rejectMarkers(local); len(files) > 0 {
		return Result{}, contentConflict(ReasonUnresolvedMarkers, "Resolve the conflict blocks before encrypting.", files)
	}
	localTree, err := git.BuildTree(n.ws.Repo(), local)
	if err != nil {
		return Result{}, invalidRequest(ReasonInvalidContent, err, nil, "visible files cannot be represented as notebook state")
	}
	if err := n.enforcePolicy(ctx, local, localTree); err != nil {
		return Result{}, err
	}
	baseTree := n.ws.Baseline().Tree

	for attempt := 1; ; attempt++ {
		remote, err := n.rootRemote(ctx, c, rekey)
		if err != nil {
			return Result{}, err
		}
		merged, err := git.Merge(n.ws.Repo(), baseTree, localTree, remote.tree)
		if err != nil {
			return Result{}, storageIntegrity(ReasonEngineFailed, err, "merge failed")
		}
		if len(merged.Conflicts) > 0 {
			tree, err := n.materializeTree(merged, localTree)
			if err != nil {
				return Result{}, err
			}
			if err := n.applyLocal(ctx, stageConflict, RemoteAcceptedNo, func() error {
				return n.ws.Materialize(ctx, remote.baseline(), tree)
			}); err != nil {
				return Result{}, err
			}
			return Result{}, contentConflict(ReasonMergeConflict, "Resolve the conflict blocks before encrypting.",
				contentConflictFiles(merged.Conflicts))
		}
		stat, err := n.diffStat(remote.tree, merged.Tree)
		if err != nil {
			return Result{}, err
		}
		proposal, err := n.buildRootProposal(remote, merged.Tree, message)
		if err != nil {
			return Result{}, err
		}
		if err := n.uploadEncrypted(ctx, c, proposal); err != nil {
			return Result{}, err
		}
		err = n.publishEncrypted(ctx, c, remote, proposal)
		if errors.Is(err, errCASLost) {
			if attempt > n.retryLimit {
				return Result{}, remoteBusy("another writer kept winning the publication race for %d attempts", attempt)
			}
			if werr := n.waiter.Wait(ctx, attempt); werr != nil {
				return Result{}, werr
			}
			continue
		}
		if err != nil {
			return Result{}, err
		}
		if err := n.ws.Accept(ctx, proposal.baseline); err != nil {
			n.cleanupCurrent(ctx, c, proposal.key.Generation)
			return Result{}, n.failAfterAccept(ctx, stageCommit, err)
		}
		if !n.ws.Pulled() {
			if err := n.ws.MarkPulled(ctx); err != nil {
				n.cleanupCurrent(ctx, c, proposal.key.Generation)
				return Result{}, n.failAfterAccept(ctx, stageCommit, err)
			}
		}
		// The superseded packs are now unreferenced. Cleanup is best effort and
		// never changes the accepted result (architecture/checkpoints.md).
		n.cleanupCurrent(ctx, c, proposal.key.Generation)
		return Result{Generation: proposal.baseline.RemoteGeneration, Stat: stat}, nil
	}
}

// cleanupCurrent deletes the packs an accepted root publication left
// unreferenced. The new current is encrypted, so cleanupPacks rereads and
// decrypts it through the publication's keyring before every delete batch. A
// cleanup failure is recorded in metrics and never changes the accepted
// result.
func (n *Notebook) cleanupCurrent(ctx context.Context, c Cipher, cutoff uint64) {
	n.cleanupPacks(ctx, cutoff, func(ctx context.Context) (map[string]struct{}, error) {
		data, _, present, err := readRawCurrent(ctx, c.Store)
		if err != nil {
			return nil, err
		}
		if !present {
			return nil, errors.New("current manifest disappeared during cleanup")
		}
		m, err := decodeStoredCurrent(ctx, c.Seal, data)
		if err != nil {
			return nil, err
		}
		return manifestRoots(m), nil
	})
}

// rootRemote reads the accepted state one root publication builds on. init
// reads the plaintext accepted state exactly as a commit does, so a concurrent
// non-conflicting publication merges into the checkpoint, and refuses a
// non-empty remote this workspace has not pulled, because conversion publishes
// the workspace baseline. rekey reads the encrypted current directly: this
// build does not import an encrypted tail, so the observed accepted state must
// be the pulled baseline.
func (n *Notebook) rootRemote(ctx context.Context, c Cipher, rekey bool) (remoteState, error) {
	if rekey {
		return n.encryptedRemote(ctx, c)
	}
	if err := n.refuseEncryptedCurrent(ctx, c.Store); err != nil {
		return remoteState{}, err
	}
	remote, err := n.readRemote(ctx)
	if err != nil {
		return remoteState{}, err
	}
	if remote.present && !n.ws.Pulled() {
		return remoteState{}, invalidRequest(ReasonPullRequired, nil, nil, "the accepted state is not in this workspace; pull it, then retry")
	}
	return remote, nil
}

// refuseEncryptedCurrent refuses init on a notebook another process already
// encrypted, before the plaintext manifest reader meets the version-two
// wrapper.
func (n *Notebook) refuseEncryptedCurrent(ctx context.Context, raw storage.ObjectStore) error {
	data, _, present, err := readRawCurrent(ctx, raw)
	if err != nil || !present {
		return err
	}
	encoding, _, err := storage.ClassifyCurrent(data)
	if err != nil {
		return manifestError(err)
	}
	if encoding == storage.CurrentEncrypted {
		return invalidRequest(ReasonEncryptionAlreadyEnabled, nil, nil, "this notebook is already encrypted")
	}
	return nil
}

// encryptedRemote reads the accepted state of an encrypted notebook without
// importing its tail: it opens current with c.Open and requires the observed
// manifest to be the workspace baseline, because the root publication
// supersedes the history this build cannot reconstruct.
func (n *Notebook) encryptedRemote(ctx context.Context, c Cipher) (remoteState, error) {
	data, etag, present, err := readRawCurrent(ctx, c.Store)
	if err != nil {
		return remoteState{}, err
	}
	if !present {
		return remoteState{}, invalidRequest(ReasonEncryptionNotEnabled, nil, nil, "the notebook is not encrypted")
	}
	encoding, envelope, err := storage.ClassifyCurrent(data)
	if err != nil {
		return remoteState{}, manifestError(err)
	}
	if encoding == storage.CurrentPlaintext {
		return remoteState{}, invalidRequest(ReasonEncryptionNotEnabled, nil, nil, "the notebook is not encrypted")
	}
	if c.Open == nil {
		return remoteState{}, storageFailure(ReasonKeyRequired, nil, "the notebook is encrypted; supply its passphrase or recovery code")
	}
	plaintext, _, err := c.Open.Open(ctx, storage.CurrentKey, envelope)
	if err != nil {
		return remoteState{}, encryptionOpenError(err)
	}
	manifest, err := storage.DecodeManifest(plaintext)
	if err != nil {
		return remoteState{}, manifestError(err)
	}
	if err := storage.CheckManifestRollback(manifest, n.ws.Baseline().RemoteGeneration); err != nil {
		return remoteState{}, storageIntegrity(ReasonHistoryInvalid, err, "current is older than the workspace baseline")
	}
	if manifest.Head != n.ws.Baseline().Head || manifest.Generation != n.ws.Baseline().RemoteGeneration {
		return remoteState{}, invalidRequest(ReasonPullRequired, nil, nil, "the accepted state moved; pull it, then retry")
	}
	commit, err := n.ws.Repo().ReadCommit(manifest.Head)
	if err != nil {
		return remoteState{}, storageIntegrity(ReasonHistoryInvalid, err, "accepted state %s is unreadable", manifest.Head)
	}
	return acceptedState(manifest, etag, commit.Tree), nil
}

// rootProposal is one encrypted root-checkpoint publication: a new root commit,
// its state-complete pack, the manifest that references it, and the accepted
// baseline it creates.
type rootProposal struct {
	publicationID storage.UUID
	key           storage.Key
	pack          git.Pack
	manifest      storage.Manifest
	baseline      workspace.Baseline
}

// buildRootProposal creates the root commit, the state-complete checkpoint
// pack, the shallow boundary, and the root manifest at the next generation.
func (n *Notebook) buildRootProposal(remote remoteState, tree git.OID, message string) (rootProposal, error) {
	pubID, err := n.newID()
	if err != nil {
		return rootProposal{}, storageFailure(ReasonInternal, err, "generate publication id")
	}
	head, err := git.CreateCommit(n.ws.Repo(), git.CommitSpec{Message: message, Tree: tree, Time: n.now()})
	if err != nil {
		return rootProposal{}, storageIntegrity(ReasonEngineFailed, err, "create the checkpoint commit")
	}
	pack, err := git.ExportCheckpoint(n.ws.Repo(), head)
	if err != nil {
		return rootProposal{}, storageIntegrity(ReasonEngineFailed, err, "prepare the full notebook state for publication")
	}
	if err := git.MarkShallow(n.ws.Repo(), head); err != nil {
		return rootProposal{}, storageIntegrity(ReasonEngineFailed, err, "record the checkpoint boundary")
	}
	cpID, err := n.newID()
	if err != nil {
		return rootProposal{}, storageFailure(ReasonInternal, err, "generate checkpoint id")
	}
	generation := remote.generation + 1
	key := storage.Key{Kind: storage.KindCheckpoint, Generation: generation, ID: cpID}
	return rootProposal{
		publicationID: pubID,
		key:           key,
		pack:          pack,
		manifest: storage.Manifest{
			Version:    storage.ManifestVersion,
			Generation: generation,
			Head:       head,
			Checkpoint: storage.Checkpoint{
				ID:                cpID,
				Publication:       pubID,
				ThroughGeneration: generation,
				Head:              head,
				Key:               key,
				SHA256:            pack.SHA256,
				Size:              uint64(len(pack.Data)),
			},
			Increments: []storage.Increment{},
			Retained:   []storage.Retained{},
		},
		baseline: workspace.Baseline{RemoteGeneration: generation, Head: head, Tree: tree},
	}, nil
}

// uploadEncrypted seals the proposal's state-complete pack under the new
// notebook key and stores the ciphertext at its unique key. The bytes precede
// any manifest reference.
func (n *Notebook) uploadEncrypted(ctx context.Context, c Cipher, p rootProposal) error {
	envelope, err := c.Seal.Seal(p.key.String(), crypt.KindPack, p.pack.Data, nil)
	if err != nil {
		return storageFailure(ReasonPackUpload, err, "encrypt the checkpoint pack")
	}
	sum := sha256.Sum256(envelope)
	meta := storage.Metadata{
		SHA256:     storage.SHA256(sum),
		Size:       uint64(len(envelope)),
		Kind:       p.key.Kind,
		Generation: p.key.Generation,
	}
	if err := storage.UploadUnique(ctx, c.Store, p.key, bytes.NewReader(envelope), meta); err != nil {
		return n.mapUploadError(err)
	}
	return nil
}

// publishEncrypted writes the root manifest with a conditional create or
// replace. The manifest body is sealed with the new key and doors by this
// layer, because the store holds the wrapped envelope verbatim. A
// precondition failure is errCASLost; a lost response is resolved by reading
// current back and searching for the proposal's publication ID.
func (n *Notebook) publishEncrypted(ctx context.Context, c Cipher, remote remoteState, p rootProposal) error {
	manifestBytes, err := storage.EncodeManifest(p.manifest)
	if err != nil {
		return storageIntegrity(ReasonEngineFailed, err, "encode the checkpoint manifest")
	}
	envelope, err := c.Seal.Seal(storage.CurrentKey, crypt.KindCurrent, manifestBytes, c.Doors)
	if err != nil {
		return storageFailure(ReasonManifestWrite, err, "encrypt the current manifest")
	}
	wrapped, err := storage.WrapCurrentEnvelope(envelope)
	if err != nil {
		return storageIntegrity(ReasonManifestInvalid, err, "encode the current wrapper")
	}
	var werr error
	if remote.present {
		_, werr = c.Store.ReplaceObject(ctx, storage.CurrentKey, remote.etag, wrapped)
	} else {
		_, werr = c.Store.CreateObject(ctx, storage.CurrentKey, wrapped)
	}
	switch {
	case werr == nil:
		return nil
	case errors.Is(werr, storage.ErrPreconditionFailed):
		return errCASLost
	case errors.Is(werr, storage.ErrTransport):
		found, lerr := n.lookupPublication(ctx, c.Seal, p.publicationID)
		if lerr != nil {
			return lerr
		}
		if found {
			return nil
		}
		return storageFailure(ReasonPublicationUnproven, werr, "manifest acceptance cannot be proved; the proposal is not republished")
	default:
		return storageFailure(ReasonManifestWrite, fmt.Errorf("%w: %w", errManifestRefused, werr), "manifest CAS failed")
	}
}

// readRawCurrent reads the current object from the raw store. ErrNotFound is
// the implicit empty-notebook state, not an error.
func readRawCurrent(ctx context.Context, raw storage.ObjectStore) ([]byte, storage.ETag, bool, error) {
	rc, info, err := raw.ReadObject(ctx, storage.CurrentKey)
	if err != nil {
		if errors.Is(err, storage.ErrNotFound) {
			return nil, "", false, nil
		}
		return nil, "", false, storageFailure(ReasonManifestRead, err, "read current manifest")
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, "", false, storageFailure(ReasonManifestRead, err, "read current manifest body")
	}
	return data, info.ETag, true, nil
}

// decodeStoredCurrent decodes a stored current object into a manifest. A
// version-two wrapper is decrypted with open; a plaintext version-one object
// needs no key. An encrypted object with no key is the documented KEY_REQUIRED
// refusal.
func decodeStoredCurrent(ctx context.Context, open *crypt.Keyring, data []byte) (storage.Manifest, error) {
	encoding, envelope, err := storage.ClassifyCurrent(data)
	if err != nil {
		return storage.Manifest{}, manifestError(err)
	}
	if encoding == storage.CurrentPlaintext {
		m, err := storage.DecodeManifest(data)
		if err != nil {
			return storage.Manifest{}, manifestError(err)
		}
		return m, nil
	}
	if open == nil {
		return storage.Manifest{}, storageFailure(ReasonKeyRequired, nil, "the notebook is encrypted; supply its passphrase or recovery code")
	}
	plaintext, _, err := open.Open(ctx, storage.CurrentKey, envelope)
	if err != nil {
		return storage.Manifest{}, encryptionOpenError(err)
	}
	m, err := storage.DecodeManifest(plaintext)
	if err != nil {
		return storage.Manifest{}, manifestError(err)
	}
	return m, nil
}

// keyMaterialError classifies a key-material refusal of the encrypted
// decorator: a process that holds no secret at all reports KEY_REQUIRED,
// and one whose secrets opened no door reports KEY_REJECTED. Both are
// permanent STORAGE_FAILURE refusals, and both leave the store untouched
// (architecture/encryption.md).
func keyMaterialError(err error) (error, bool) {
	switch {
	case errors.Is(err, storage.ErrKeyRequired):
		return storageFailure(ReasonKeyRequired, nil, "the notebook is encrypted and no passphrase was supplied; run pull or commit in a terminal to be asked, or supply the key material through the environment or the key record"), true
	case errors.Is(err, crypt.ErrKeyRejected):
		return storageFailure(ReasonKeyRejected, nil, "no supplied secret opens this encrypted notebook"), true
	}
	return nil, false
}

// encryptionOpenError classifies a crypt failure at the encryption-command
// boundary. A secret that opens no door is a key-material refusal, not
// corruption; an unsupported envelope is this build's UPGRADE_REQUIRED; every
// other framing or authentication failure is stored state that failed
// validation.
func encryptionOpenError(err error) error {
	switch {
	case errors.Is(err, crypt.ErrKeyRejected):
		return storageFailure(ReasonKeyRejected, nil, "no supplied secret opens this notebook")
	case errors.Is(err, crypt.ErrUnsupportedFormat):
		return storageFailure(ReasonUpgradeRequired, err, "the notebook envelope needs a newer slivingdoc")
	default:
		return storageIntegrity(ReasonEnvelopeInvalid, err, "the stored notebook envelope is not valid")
	}
}

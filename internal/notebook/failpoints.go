package notebook

// Failpoints injects deterministic failures at the notebook orchestration
// boundaries. A nil hook means no injection. The workspace mutation
// boundaries keep their own failpoints; the notebook maps their errors to
// the generic recovery path.
type Failpoints struct {
	// CAS fires after the manifest CAS accepted the proposal and before
	// the local acceptance begins. A failure leaves the remote accepted
	// and P/L unadvanced: the exact window the generic recovery path
	// repairs, with remote acceptance known to be yes.
	CAS func() error
}

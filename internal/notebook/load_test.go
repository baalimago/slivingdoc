package notebook

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/baalimago/slivingdoc/internal/storage"
	"github.com/baalimago/slivingdoc/internal/storage/fake"
	"github.com/baalimago/slivingdoc/internal/workspace"
)

// The load harness measures the planning workload of architecture section
// 16: concurrent writers that each add approximately 1 kB of new note
// content, with either distributed timing (the once-per-minute cadence) or
// one synchronized burst. It runs against the deterministic fake store and
// fake engine, so the measured CAS contention and conflict counts are
// reproducible without Docker, network, or CI noise; the MinIO suite
// separately proves the same operations over real HTTP.

// loadSchedule selects the arrival pattern of one load run.
type loadSchedule int

const (
	// scheduleDistributed staggers the writers over the configured window,
	// simulating the planning cadence of one commit per writer per minute
	// compressed for the benchmark.
	scheduleDistributed loadSchedule = iota
	// scheduleBurst releases every writer at once from the same baseline.
	scheduleBurst
)

// loadConfig describes one load run. Writers is the concurrent-writer count
// (the planning workload is 100); NoteBytes is the approximate size of the
// new note content per commit; CheckpointPacks configures the checkpoint
// threshold during the run so the harness also exercises compaction and
// cleanup (retention keeps the default previous generation);
// DistributedWindow is the wall-clock window over which the distributed
// schedule spreads the writers; RetryLimit is the CAS retry bound (0
// resolves to the production default).
type loadConfig struct {
	writers           int
	noteBytes         int
	checkpointPacks   int
	distributedWindow time.Duration
	retryLimit        int
	schedule          loadSchedule
}

// loadResult is the measured outcome of one load run: accepted commits,
// failures, conflicts, wall duration and throughput, latency percentiles
// (over every completed commit call), CAS attempts per accepted commit, and
// the checkpoint and cleanup results. Store lets a caller reconstruct and
// verify the final accepted state.
type loadResult struct {
	store     storage.ObjectStore
	commits   int
	failures  int
	conflicts int
	elapsed   time.Duration
	latency   []time.Duration
	casTotal  int

	tailCount uint64
	tailBytes uint64

	checkpointRuns     uint64
	checkpointFailures uint64
	checkpointSize     uint64
	cleanupRuns        uint64
	cleanupDeleted     uint64
	cleanupErrors      uint64
}

// throughput is the accepted-commits-per-second rate of the run.
func (r loadResult) throughput() float64 {
	if r.elapsed == 0 {
		return 0
	}
	return float64(r.commits) / r.elapsed.Seconds()
}

// casPerCommit is the manifest CAS attempts per accepted commit: the
// publication-efficiency measure of architecture section 16.
func (r loadResult) casPerCommit() float64 {
	if r.commits == 0 {
		return 0
	}
	return float64(r.casTotal) / float64(r.commits)
}

// percentile returns the p-th percentile (0..100) of the latency samples
// using the nearest-rank method, or 0 for an empty sample.
func percentile(latency []time.Duration, p int) time.Duration {
	if len(latency) == 0 {
		return 0
	}
	sorted := append([]time.Duration(nil), latency...)
	slices.Sort(sorted)
	idx := min(max((p*len(sorted)+99)/100, 1), len(sorted))
	return sorted[idx-1]
}

// runWriterLoad executes one load plan against a fresh fake store: every
// writer pulls once at its scheduled arrival time, appends its own
// approximately-NoteBytes note file, and commits once. The final accepted
// state contains every writer's note. The fake store serializes each CAS,
// so the burst schedule loses exactly the number of races the retry bound
// permits and the run is reproducible.
func runWriterLoad(tb testing.TB, cfg loadConfig) loadResult {
	if cfg.writers < 1 {
		tb.Fatalf("runWriterLoad: writers %d, want at least 1", cfg.writers)
	}
	if cfg.checkpointPacks == 0 {
		cfg.checkpointPacks = DefaultCheckpointPacks
	}
	store := fake.New("")
	ids := &testIDSource{}

	type worker struct {
		nb *Notebook
		w  *workspace.Workspace
	}
	workers := make([]worker, cfg.writers)
	for i := range workers {
		nb, w, _ := newNotebook(tb, nbConfig{
			store:           store,
			ids:             ids,
			retryLimit:      cfg.retryLimit,
			checkpointPacks: cfg.checkpointPacks,
			retained:        DefaultRetainedCheckpoints,
			retainedSet:     true,
		})
		workers[i] = worker{nb: nb, w: w}
	}

	start := time.Now()
	results := make(chan commitOutcome, cfg.writers)
	var wg sync.WaitGroup
	for i, wk := range workers {
		wg.Go(func() {
			if cfg.schedule == scheduleDistributed {
				stagger := cfg.distributedWindow / time.Duration(cfg.writers)
				time.Sleep(time.Duration(i) * stagger)
			}
			if err := wk.nb.Pull(context.Background()); err != nil {
				results <- commitOutcome{err: err}
				return
			}
			path := fmt.Sprintf("agent-%03d.md", i)
			writeLocal(tb, wk.w, map[string]string{path: noteBody(i)})
			commitStart := time.Now()
			err := wk.nb.Commit(context.Background(), "load commit")
			results <- commitOutcome{err: err, duration: time.Since(commitStart)}
		})
	}
	wg.Wait()
	close(results)
	elapsed := time.Since(start)

	res := loadResult{store: store, elapsed: elapsed}
	for out := range results {
		if out.err == nil {
			res.commits++
			res.latency = append(res.latency, out.duration)
			continue
		}
		var ne *Error
		if errors.As(out.err, &ne) && ne.Code == CodeContentConflict {
			res.conflicts++
		} else {
			res.failures++
		}
		res.latency = append(res.latency, out.duration)
	}

	// CAS attempts: every conditional create and replacement, including
	// checkpoint manifest updates.
	res.casTotal = store.Calls(fake.OpCreate) + store.Calls(fake.OpReplace)

	// The active tail shape and the checkpoint and cleanup results come
	// from the final manifest and from the aggregated notebook metrics.
	m := readManifest(tb, store)
	res.tailCount = uint64(len(m.Increments))
	for _, inc := range m.Increments {
		res.tailBytes += inc.Size
	}
	for _, wk := range workers {
		metrics := wk.nb.Metrics()
		res.checkpointRuns += metrics.CheckpointRuns.Load()
		res.checkpointFailures += metrics.CheckpointFailures.Load()
		res.checkpointSize = max(res.checkpointSize, metrics.CheckpointSize.Load())
		res.cleanupRuns += metrics.CleanupRuns.Load()
		res.cleanupDeleted += metrics.CleanupDeleted.Load()
		res.cleanupErrors += metrics.CleanupErrors.Load()
	}
	return res
}

// commitOutcome is one writer's completed commit call.
type commitOutcome struct {
	err      error
	duration time.Duration
}

// noteBody renders the approximately-1 kB note content of one commit.
func noteBody(i int) string {
	return fmt.Sprintf("note %d\n%s", i, strings.Repeat("x", 1024))
}

// TestLoadHarnessRecordsThroughputLatencyCASAndConflicts proves the load
// acceptance criterion: a run records accepted commits, throughput, latency
// percentiles, CAS attempts, and conflicts for both arrival patterns, and
// the final accepted state reconstructs every writer's note. Nine writers
// fit the production retry bound even in the fully overlapping worst case,
// so the run is deterministic.
func TestLoadHarnessRecordsThroughputLatencyCASAndConflicts(t *testing.T) {
	for _, tc := range []struct {
		name     string
		schedule loadSchedule
		window   time.Duration
	}{
		{name: "distributed", schedule: scheduleDistributed, window: 180 * time.Millisecond},
		{name: "burst", schedule: scheduleBurst},
	} {
		t.Run(tc.name, func(t *testing.T) {
			res := runWriterLoad(t, loadConfig{
				writers:           9,
				noteBytes:         1024,
				checkpointPacks:   8,
				distributedWindow: tc.window,
				schedule:          tc.schedule,
			})
			if res.commits != 9 || res.failures != 0 {
				t.Fatalf("load = %d accepted %d failed, want 9 accepted 0 failed", res.commits, res.failures)
			}
			if res.conflicts != 0 {
				t.Fatalf("conflicts = %d, want 0 for disjoint writer files", res.conflicts)
			}
			if res.elapsed <= 0 || res.throughput() <= 0 {
				t.Fatalf("elapsed %v throughput %v, want positive measurements", res.elapsed, res.throughput())
			}
			if len(res.latency) != 9 {
				t.Fatalf("latency samples = %d, want 9", len(res.latency))
			}
			p50, p90, p99 := percentile(res.latency, 50), percentile(res.latency, 90), percentile(res.latency, 99)
			if p50 <= 0 || p50 > p90 || p90 > p99 {
				t.Fatalf("latency percentiles p50=%v p90=%v p99=%v, want 0 < p50 <= p90 <= p99", p50, p90, p99)
			}
			if res.casTotal < res.commits {
				t.Fatalf("CAS attempts = %d, want at least one per accepted commit (%d)", res.casTotal, res.commits)
			}
			if tc.schedule == scheduleBurst && res.casTotal <= res.commits {
				t.Fatalf("burst CAS attempts = %d for %d commits, want contention above one attempt per commit", res.casTotal, res.commits)
			}
			if res.checkpointRuns == 0 || res.checkpointSize == 0 || res.cleanupRuns == 0 {
				t.Fatalf("checkpoint/cleanup metrics = runs %d size %d cleanup %d, want a completed compaction",
					res.checkpointRuns, res.checkpointSize, res.cleanupRuns)
			}

			// A fresh reader reconstructs the complete final state: every
			// writer's note is present in the accepted head.
			reader, rw, _ := newNotebook(t, nbConfig{store: res.store, ids: &testIDSource{}})
			pullOK(t, reader)
			snap := localSnapshot(t, rw)
			for i := range 9 {
				path := fmt.Sprintf("agent-%03d.md", i)
				if _, ok := snap[path]; !ok {
					t.Fatalf("final state missing %s", path)
				}
			}
		})
	}
}

// BenchmarkCommitLoadDistributed runs the planning workload with
// distributed timing: 100 writers each add approximately 1 kB once per
// minute (the 60-second window compressed to 2 seconds), so the measured
// arrival pattern has the planned cadence and near-zero CAS contention.
func BenchmarkCommitLoadDistributed(b *testing.B) {
	for i := 0; i < b.N; i++ {
		res := runWriterLoad(b, loadConfig{
			writers:           100,
			noteBytes:         1024,
			checkpointPacks:   50,
			distributedWindow: 2 * time.Second,
			schedule:          scheduleDistributed,
		})
		reportLoad(b, res)
	}
}

// BenchmarkCommitLoadBurst runs the synchronized burst: 100 writers fire at
// once from the same baseline with the production retry bound, so the
// measured result records exactly how many commits the bounded CAS retries
// absorb and the contention cost in CAS attempts per accepted commit.
func BenchmarkCommitLoadBurst(b *testing.B) {
	for i := 0; i < b.N; i++ {
		res := runWriterLoad(b, loadConfig{
			writers:         100,
			noteBytes:       1024,
			checkpointPacks: 50,
			schedule:        scheduleBurst,
		})
		reportLoad(b, res)
	}
}

// reportLoad records one load run's measured results as benchmark metrics:
// throughput, CAS attempts per accepted commit, conflicts, failures, and
// the commit-latency percentiles over every completed commit call.
func reportLoad(b *testing.B, res loadResult) {
	b.ReportMetric(res.throughput(), "commits/sec")
	b.ReportMetric(res.casPerCommit(), "cas/commit")
	b.ReportMetric(float64(res.conflicts), "conflicts")
	b.ReportMetric(float64(res.failures), "failed")
	b.ReportMetric(float64(res.checkpointRuns), "checkpoint-runs")
	b.ReportMetric(float64(res.cleanupDeleted), "cleanup-deleted")
	b.ReportMetric(float64(percentile(res.latency, 50))/float64(time.Millisecond), "p50-ms")
	b.ReportMetric(float64(percentile(res.latency, 90))/float64(time.Millisecond), "p90-ms")
	b.ReportMetric(float64(percentile(res.latency, 99))/float64(time.Millisecond), "p99-ms")
}

// seededState commits a checkpointed history into a fresh fake store and
// returns the store and the total pack bytes a cold reader must download.
func seededState(tb testing.TB, commits, threshold int) (*fake.Store, uint64) {
	store := fake.New("")
	ids := &testIDSource{}
	seed, sw, _ := newNotebook(tb, nbConfig{store: store, ids: ids, checkpointPacks: threshold})
	writeLocal(tb, sw, map[string]string{"seed.md": "seed"})
	pullOK(tb, seed)
	for i := 1; i <= commits; i++ {
		writeLocal(tb, sw, map[string]string{fmt.Sprintf("seed-%03d.md", i): noteBody(i)})
		commitOK(tb, seed, "seed commit")
	}
	m := readManifest(tb, store)
	total := m.Checkpoint.Size
	for _, inc := range m.Increments {
		total += inc.Size
	}
	return store, total
}

// BenchmarkPullCold measures the cold-reader path: a fresh workspace with
// an empty pack cache pulls a checkpointed state with a tail, so every
// referenced pack is downloaded, verified, cached, and imported.
func BenchmarkPullCold(b *testing.B) {
	store, total := seededState(b, 150, 100)
	ids := &testIDSource{}
	b.ReportAllocs()
	b.SetBytes(int64(total))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		reader, _, _ := newNotebook(b, nbConfig{store: store, ids: ids})
		pullOK(b, reader)
	}
}

// BenchmarkPullWarm measures the warm-reader path: a reader whose pack
// cache already holds the base state pulls one new increment, so exactly
// one pack is downloaded per pull.
func BenchmarkPullWarm(b *testing.B) {
	store := fake.New("")
	ids := &testIDSource{}
	seed, sw, _ := newNotebook(b, nbConfig{store: store, ids: ids, checkpointPacks: 1000})
	writeLocal(b, sw, map[string]string{"seed.md": "seed"})
	pullOK(b, seed)
	for i := 1; i <= 100; i++ {
		writeLocal(b, sw, map[string]string{fmt.Sprintf("warm-%03d.md", i): noteBody(i)})
		commitOK(b, seed, "seed commit")
	}
	reader, _, _ := newNotebook(b, nbConfig{store: store, ids: ids})
	pullOK(b, reader) // warm the pack cache

	b.ReportAllocs()
	b.ResetTimer()
	for i := 1; i <= b.N; i++ {
		writeLocal(b, sw, map[string]string{fmt.Sprintf("warm-new-%04d.md", i): noteBody(i)})
		b.StopTimer()
		commitOK(b, seed, "warm commit")
		m := readManifest(b, store)
		b.SetBytes(int64(m.Increments[len(m.Increments)-1].Size))
		b.StartTimer()
		pullOK(b, reader)
	}
}

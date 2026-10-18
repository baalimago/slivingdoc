// Command encryption-perf measures the completed client-side encryption
// against the budgets of the encrypted-notebooks effort
// (worklogs/26-10-05-encrypted-notebooks/phase-1-crypto-core.md). It runs on
// the named reference host, never contacts a store, and exits nonzero when
// any budget fails.
package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/baalimago/slivingdoc/internal/crypt"
	"github.com/baalimago/slivingdoc/internal/httpstore/gatewaytest"
)

// The measured budgets.
const (
	budgetUnlock     = 15 * time.Second
	budgetPairUnlock = 30 * time.Second
	budgetPairRSS    = 1 << 30
	budgetThroughput = 100 << 20
	budgetFramingRSS = 350 << 20
)

const (
	// trials is the number of independent trials behind every median.
	trials = 5
	// candidates is the size of the measured candidate-door search: one
	// rejected candidate and one accepted candidate.
	candidates = 2
	objectKey  = "current"
	passphrase = "encryption performance passphrase"
	note       = "performance envelope"
)

// measurement is one quantity with its budget.
type measurement struct {
	name   string
	value  string
	budget string
	ok     bool
	detail string
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "encryption-perf:", err)
		os.Exit(1)
	}
}

func run() error {
	reportEnvironment()

	gate, err := crypt.NewKDFGate(1)
	if err != nil {
		return err
	}
	key, err := crypt.GenerateNotebookKey()
	if err != nil {
		return err
	}
	keyring, err := crypt.NewUnlockedKeyring(key, nil, gate)
	if err != nil {
		return err
	}
	door, err := crypt.WrapKey(context.Background(), gate, key, []byte(passphrase), objectKey, crypt.DoorPassphrase, crypt.DefaultKDFParams())
	if err != nil {
		return err
	}
	envelope, err := keyring.Seal(objectKey, crypt.KindCurrent, []byte(note), []crypt.Door{door})
	if err != nil {
		return err
	}

	unlock := func() error {
		secrets := make([]crypt.Candidate, 0, candidates)
		for i := range candidates - 1 {
			secrets = append(secrets, crypt.Candidate{Source: crypt.SourcePrompt, Secret: []byte("rejected candidate " + strconv.Itoa(i))})
		}
		secrets = append(secrets, crypt.Candidate{Source: crypt.SourceEnvironment, Secret: []byte(passphrase)})
		ring, err := crypt.NewKeyring(secrets, gate)
		if err != nil {
			return err
		}
		plaintext, _, err := ring.Open(context.Background(), objectKey, envelope)
		if err != nil {
			return err
		}
		if string(plaintext) != note {
			return errors.New("unlock returned different bytes")
		}
		return nil
	}

	// Framing runs first, on a fresh heap: its budget is the framing
	// operation's own footprint, and a preceding Argon2id phase would leave
	// its 256 MiB buffer resident. Every phase starts from a released heap.
	debug.FreeOSMemory()
	pattern := []byte("slivingdoc encryption framing payload; ")
	payload := bytes.Repeat(pattern, gatewaytest.MaxPackBytes/len(pattern)+1)[:gatewaytest.MaxPackBytes]
	encrypt, decrypt, encryptRSS, decryptRSS, err := measureFraming(keyring, door, payload)
	if err != nil {
		return err
	}

	debug.FreeOSMemory()
	single, err := measureTrials(unlock)
	if err != nil {
		return err
	}

	debug.FreeOSMemory()
	pair, pairRSS, err := measurePair(unlock)
	if err != nil {
		return err
	}

	results := []measurement{
		timeBudget("one isolated full unlock", median(single), budgetUnlock, fmt.Sprintf("%d candidates against each door; trials %s", candidates, durations(single))),
		timeBudget("two simultaneous full unlocks", median(pair), budgetPairUnlock, fmt.Sprintf("serialized through one gate slot; trials %s", durations(pair))),
		sizeBudget("paired full-unlock peak RSS", pairRSS, budgetPairRSS, ""),
		rateBudget("framing encryption throughput", median(encrypt), func(d time.Duration) float64 {
			return float64(len(payload)) / d.Seconds()
		}, fmt.Sprintf("trials %s", durations(encrypt))),
		rateBudget("framing decryption throughput", median(decrypt), func(d time.Duration) float64 {
			return float64(len(payload)) / d.Seconds()
		}, fmt.Sprintf("trials %s", durations(decrypt))),
		sizeBudget("framing encryption peak RSS", encryptRSS, budgetFramingRSS, ""),
		sizeBudget("framing decryption peak RSS", decryptRSS, budgetFramingRSS, ""),
	}

	fmt.Printf("framing payload: %s (%d bytes)\n\n", mebibytes(uint64(len(payload))), len(payload))

	failed := false
	for _, m := range results {
		verdict := "PASS"
		if !m.ok {
			verdict = "FAIL"
			failed = true
		}
		fmt.Printf("%-4s %-34s %12s  budget %-18s %s\n", verdict, m.name, m.value, m.budget, m.detail)
	}
	if failed {
		return errors.New("a performance budget failed")
	}
	fmt.Println("\nevery budget passed")
	return nil
}

// measureTrials runs the operation trials times and returns every elapsed
// time.
func measureTrials(operation func() error) ([]time.Duration, error) {
	elapsed := make([]time.Duration, 0, trials)
	for range trials {
		start := time.Now()
		if err := operation(); err != nil {
			return nil, err
		}
		elapsed = append(elapsed, time.Since(start))
	}
	return elapsed, nil
}

// measurePair runs two operations at once trials times and returns every
// elapsed time and the maximum resident set seen while they ran.
func measurePair(operation func() error) ([]time.Duration, uint64, error) {
	elapsed := make([]time.Duration, 0, trials)
	var peak uint64
	for range trials {
		sampler := startRSS()
		start := time.Now()
		errs := make(chan error, 2)
		var group sync.WaitGroup
		for range 2 {
			group.Go(func() {
				errs <- operation()
			})
		}
		group.Wait()
		close(errs)
		elapsed = append(elapsed, time.Since(start))
		peak = max(peak, sampler.stopAndPeak())
		for err := range errs {
			if err != nil {
				return nil, 0, err
			}
		}
	}
	return elapsed, peak, nil
}

// measureFraming seals and opens the framing payload trials times and
// returns every elapsed time and the peak resident set of each direction.
func measureFraming(keyring *crypt.Keyring, door crypt.Door, plaintext []byte) ([]time.Duration, []time.Duration, uint64, uint64, error) {
	encrypt := make([]time.Duration, 0, trials)
	decrypt := make([]time.Duration, 0, trials)
	var encryptPeak, decryptPeak uint64
	for range trials {
		// Release the previous trial's buffers so each trial measures its
		// own footprint rather than the process high-water mark.
		debug.FreeOSMemory()
		sampler := startRSS()
		start := time.Now()
		envelope, err := keyring.Seal(objectKey, crypt.KindPack, plaintext, []crypt.Door{door})
		encrypt = append(encrypt, time.Since(start))
		encryptPeak = max(encryptPeak, sampler.stopAndPeak())
		if err != nil {
			return nil, nil, 0, 0, err
		}

		debug.FreeOSMemory()
		sampler = startRSS()
		start = time.Now()
		opened, _, err := keyring.Open(context.Background(), objectKey, envelope)
		decrypt = append(decrypt, time.Since(start))
		decryptPeak = max(decryptPeak, sampler.stopAndPeak())
		if err != nil {
			return nil, nil, 0, 0, err
		}
		if !bytes.Equal(opened, plaintext) {
			return nil, nil, 0, 0, errors.New("framing round trip returned different bytes")
		}
	}
	return encrypt, decrypt, encryptPeak, decryptPeak, nil
}

func timeBudget(name string, got, budget time.Duration, detail string) measurement {
	return measurement{name: name, value: round(got), budget: round(budget), ok: got <= budget, detail: detail}
}

func rateBudget(name string, got time.Duration, rate func(time.Duration) float64, detail string) measurement {
	return measurement{
		name:   name,
		value:  fmt.Sprintf("%s/s", mebibytes(uint64(rate(got)))),
		budget: fmt.Sprintf("%s/s", mebibytes(budgetThroughput)),
		ok:     rate(got) >= budgetThroughput,
		detail: detail,
	}
}

func sizeBudget(name string, got, budget uint64, detail string) measurement {
	return measurement{name: name, value: mebibytes(got), budget: mebibytes(budget), ok: got <= budget, detail: detail}
}

// median returns the median of the trials.
func median(times []time.Duration) time.Duration {
	sorted := slices.Clone(times)
	slices.Sort(sorted)
	return sorted[len(sorted)/2]
}

func durations(times []time.Duration) string {
	parts := make([]string, 0, len(times))
	for _, d := range times {
		parts = append(parts, round(d))
	}
	return "[" + strings.Join(parts, " ") + "]"
}

func round(d time.Duration) string {
	if d >= time.Second {
		return fmt.Sprintf("%.3f s", d.Seconds())
	}
	return fmt.Sprintf("%.1f ms", float64(d.Microseconds())/1000)
}

func mebibytes(n uint64) string {
	return fmt.Sprintf("%.1f MiB", float64(n)/(1<<20))
}

func reportEnvironment() {
	fmt.Println("slivingdoc encryption performance")
	fmt.Println("go:", runtime.Version())
	fmt.Println("crypto:", moduleVersion("golang.org/x/crypto"))
	fmt.Println("logical CPUs:", runtime.NumCPU())
}

func moduleVersion(path string) string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	for _, dep := range info.Deps {
		if dep.Path == path {
			return dep.Version
		}
	}
	return "unknown"
}

// rssSampler polls the process resident set so a phase that allocates and
// frees a large buffer reports its true peak.
type rssSampler struct {
	stop chan struct{}
	done chan struct{}
	peak atomic.Uint64
}

func startRSS() *rssSampler {
	sampler := &rssSampler{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(sampler.done)
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			if n, err := residentBytes(); err == nil {
				sampler.peak.Store(max(sampler.peak.Load(), n))
			}
			select {
			case <-sampler.stop:
				return
			case <-ticker.C:
			}
		}
	}()
	return sampler
}

func (s *rssSampler) stopAndPeak() uint64 {
	close(s.stop)
	<-s.done
	return s.peak.Load()
}

// residentBytes reads the process resident set from the Linux proc
// filesystem, the reference host's interface.
func residentBytes() (uint64, error) {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0, err
	}
	for line := range strings.Lines(string(data)) {
		value, ok := strings.CutPrefix(line, "VmRSS:")
		if !ok {
			continue
		}
		fields := strings.Fields(value)
		if len(fields) == 0 {
			return 0, errors.New("proc status: empty VmRSS")
		}
		kib, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("proc status VmRSS: %w", err)
		}
		return kib << 10, nil
	}
	return 0, errors.New("proc status: no VmRSS")
}

package app

import (
	"fmt"
	"time"

	"github.com/baalimago/slivingdoc/internal/notebook"
	"github.com/baalimago/slivingdoc/internal/tui"
)

// WithProgress runs op, one pull or commit of the CLI, while a progress
// line on a terminal's stderr names what runs, where, and for how long
// (architecture/tui.md). Stdout keeps only the report, and a stderr that
// is not a terminal gets nothing.
func (r *Runtime) WithProgress(verb, path string, op func() (notebook.Result, error)) (notebook.Result, error) {
	s := tui.Detect(r.p.stderr, EnvLookup(r.p.env))
	start := time.Now()
	target := r.target()
	sp := s.Spin(r.p.stderr, func() string {
		return fmt.Sprintf("%s %s %s", verb, r.resolve(path), s.Dim(fmt.Sprintf("· %s · %.1fs", target, time.Since(start).Seconds())))
	})
	result, err := op()
	// The progress line is decoration: a terminal that refuses it never
	// changes the outcome of the operation it decorated.
	_ = sp.Stop()
	return result, err
}

// target names the store the notebook syncs with.
func (r *Runtime) target() string {
	if r.cfg.hosted() {
		return "space " + r.cfg.bucket
	}
	return "bucket " + r.cfg.bucket
}

package integrationtest

import (
	"context"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
)

// LogCapture records every emitted log record into one per-harness buffer.
// The harness logger is the only logger of one harness's server, so records
// are inherently scoped: a parallel harness with its own capture can never
// observe another harness's records. The app's tool-call records carry the
// mcpReqID attribute (architecture section 2, L26), and every record of one
// call shares the same mcpReqID, so the capture proves correlation and
// scoping.
type LogCapture struct {
	mu      sync.Mutex
	records []Record
}

// Record is one captured log record: the level, message, and the flattened
// attribute map. Keys are group-qualified with dots, and a call-site attr
// wins over a bound attr of the same key, matching slog.
type Record struct {
	Level slog.Level
	Msg   string
	Attrs map[string]any
}

// NewLogCapture returns an empty capture.
func NewLogCapture() *LogCapture {
	return &LogCapture{}
}

// Logger returns a slog logger writing into the capture.
func (c *LogCapture) Logger() *slog.Logger { return slog.New(c.Handler()) }

// Handler returns the slog handler writing into the capture.
func (c *LogCapture) Handler() slog.Handler { return &captureHandler{capture: c} }

// append stores one finished record.
func (c *LogCapture) append(rec Record) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.records = append(c.records, rec)
}

// Records returns a copy of every captured record in emission order.
func (c *LogCapture) Records() []Record {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]Record, len(c.records))
	copy(out, c.records)
	return out
}

// Warnings returns the records at WARN level or above.
func (c *LogCapture) Warnings() []Record {
	var out []Record
	for _, r := range c.Records() {
		if r.Level >= slog.LevelWarn {
			out = append(out, r)
		}
	}
	return out
}

// ToolCalls returns the records carrying the "tool" attribute (the
// tool-call started/completed pairs).
func (c *LogCapture) ToolCalls() []Record {
	var out []Record
	for _, r := range c.Records() {
		if _, ok := r.Attrs["tool"]; ok {
			out = append(out, r)
		}
	}
	return out
}

// DistinctReqIDs returns the sorted distinct mcpReqID values of the
// captured records.
func (c *LogCapture) DistinctReqIDs() []string {
	seen := map[string]bool{}
	for _, r := range c.Records() {
		if id, ok := r.Attrs["mcpReqID"].(string); ok && id != "" {
			seen[id] = true
		}
	}
	out := make([]string, 0, len(seen))
	for id := range seen {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// String renders every record as one text line, for diagnostics and the
// forbidden-substring redaction scans.
func (c *LogCapture) String() string {
	var sb strings.Builder
	for _, r := range c.Records() {
		sb.WriteString(r.Level.String())
		sb.WriteString(" ")
		sb.WriteString(r.Msg)
		for _, key := range sortedKeys(r.Attrs) {
			fmt.Fprintf(&sb, " %s=%v", key, r.Attrs[key])
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// captureHandler is the slog.Handler writing into one capture. It carries
// the attrs bound by With, already group-qualified, and the open group
// path. Handle flattens the bound attrs first and the record's own attrs
// second, so a call-site attr wins over a bound attr of the same key.
type captureHandler struct {
	capture *LogCapture
	attrs   []slog.Attr // group-qualified at bind time
	groups  []string
}

// Enabled always reports true: the capture keeps every record.
func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	rec := Record{Level: r.Level, Msg: r.Message, Attrs: map[string]any{}}
	for _, a := range h.attrs {
		flattenAttr(rec.Attrs, "", a)
	}
	prefix := groupPrefix(h.groups)
	r.Attrs(func(a slog.Attr) bool {
		flattenAttr(rec.Attrs, prefix, a)
		return true
	})
	h.capture.append(rec)
	return nil
}

func (h *captureHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	if len(attrs) == 0 {
		return h
	}
	prefix := groupPrefix(h.groups)
	bound := make([]slog.Attr, 0, len(h.attrs)+len(attrs))
	bound = append(bound, h.attrs...)
	for _, a := range attrs {
		bound = append(bound, slog.Attr{Key: prefix + a.Key, Value: a.Value})
	}
	return &captureHandler{capture: h.capture, attrs: bound, groups: h.groups}
}

func (h *captureHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	groups := make([]string, 0, len(h.groups)+1)
	groups = append(groups, h.groups...)
	groups = append(groups, name)
	return &captureHandler{capture: h.capture, attrs: h.attrs, groups: groups}
}

// groupPrefix renders the open group path as a dotted key prefix.
func groupPrefix(groups []string) string {
	if len(groups) == 0 {
		return ""
	}
	return strings.Join(groups, ".") + "."
}

// flattenAttr resolves one attr into the flat map, expanding groups into
// dotted keys. Resolving matters for redaction: a value hidden behind a
// LogValuer must still be scanned.
func flattenAttr(out map[string]any, prefix string, a slog.Attr) {
	value := a.Value.Resolve()
	if value.Kind() == slog.KindGroup {
		group := value.Group()
		if a.Key == "" {
			for _, sub := range group {
				flattenAttr(out, prefix, sub)
			}
			return
		}
		for _, sub := range group {
			flattenAttr(out, prefix+a.Key+".", sub)
		}
		return
	}
	if a.Key == "" {
		return
	}
	out[prefix+a.Key] = value.Any()
}

func sortedKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

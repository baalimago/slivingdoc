package workspace

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fixtureState returns a valid generation-1 record.
func fixtureState() state {
	return state{
		Version:          1,
		Identity:         strings.Repeat("a", 64),
		RemoteGeneration: 1,
		BaselineHead:     strings.Repeat("b", 40),
		BaselineTree:     EmptyTreeID.String(),
	}
}

func TestStateRoundTrip(t *testing.T) {
	st := fixtureState()
	data, err := encodeState(st)
	if err != nil {
		t.Fatalf("encodeState() = %v", err)
	}
	if strings.Contains(string(data), "\n") {
		t.Fatal("encoded state contains a newline")
	}
	back, err := decodeState(data)
	if err != nil {
		t.Fatalf("decodeState() = %v", err)
	}
	if back != st {
		t.Fatalf("round trip changed the record:\n%s", data)
	}
}

func TestStateFieldOrder(t *testing.T) {
	data, err := encodeState(fixtureState())
	if err != nil {
		t.Fatalf("encodeState() = %v", err)
	}
	const wantPrefix = `{"version":1,"identity":`
	if !strings.HasPrefix(string(data), wantPrefix) {
		t.Fatalf("encoded state %s does not start with %s", data, wantPrefix)
	}
	// The normative field order: version, identity, remoteGeneration,
	// baselineHead, baselineTree, recoveryRequired.
	order := []string{`"version":`, `"identity":`, `"remoteGeneration":`, `"baselineHead":`, `"baselineTree":`, `"recoveryRequired":`}
	last := -1
	for _, field := range order {
		i := strings.Index(string(data), field)
		if i < 0 {
			t.Fatalf("encoded state %s is missing %s", data, field)
		}
		if i <= last {
			t.Fatalf("encoded state %s breaks the field order at %s", data, field)
		}
		last = i
	}
}

func TestStateGenerationZeroRecord(t *testing.T) {
	st := newWorkspaceState(strings.Repeat("a", 64))
	if st.Version != 1 || st.RemoteGeneration != 0 || st.BaselineHead != "" || st.BaselineTree != EmptyTreeID.String() || st.RecoveryRequired {
		t.Fatalf("newWorkspaceState() = %+v", st)
	}
	data, err := encodeState(st)
	if err != nil {
		t.Fatalf("encodeState() = %v", err)
	}
	if !strings.Contains(string(data), `"baselineHead":""`) {
		t.Fatalf("generation-0 record must have an empty baselineHead: %s", data)
	}
	back, err := decodeState(data)
	if err != nil {
		t.Fatalf("decodeState() = %v", err)
	}
	if back != st {
		t.Fatalf("round trip changed the generation-0 record")
	}
}

// invalidStateCases maps a case name to a mutation that makes the fixture
// record invalid.
func invalidStateCases() map[string]func(*state) {
	return map[string]func(*state){
		"version zero":   func(s *state) { s.Version = 0 },
		"version two":    func(s *state) { s.Version = 2 },
		"short identity": func(s *state) { s.Identity = strings.Repeat("a", 63) },
		"upper identity": func(s *state) { s.Identity = strings.ToUpper(strings.Repeat("a", 64)) },
		"empty identity": func(s *state) { s.Identity = "" },
		"head at gen zero": func(s *state) {
			s.RemoteGeneration = 0
			s.BaselineHead = strings.Repeat("b", 40)
		},
		"empty head past zero": func(s *state) {
			s.RemoteGeneration = 1
			s.BaselineHead = ""
		},
		"bad head":   func(s *state) { s.BaselineHead = "xyz" },
		"empty tree": func(s *state) { s.BaselineTree = "" },
		"bad tree":   func(s *state) { s.BaselineTree = strings.Repeat("g", 40) },
	}
}

func TestValidateStateRejects(t *testing.T) {
	for name, mutate := range invalidStateCases() {
		t.Run(name, func(t *testing.T) {
			st := fixtureState()
			mutate(&st)
			if _, err := encodeState(st); err == nil {
				t.Fatalf("encodeState() succeeded for %s", name)
			}
			data, err := encodeState(fixtureState())
			if err != nil {
				t.Fatalf("encode base: %v", err)
			}
			if _, err := decodeState(data); err != nil {
				t.Fatalf("base record must decode: %v", err)
			}
		})
	}
}

// stateDoc builds a JSON state document with the fixture values.
func stateDoc(fields string) string {
	return `{"version":1,"identity":"` + strings.Repeat("a", 64) +
		`","remoteGeneration":1,"baselineHead":"` + strings.Repeat("b", 40) +
		`","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false` + fields + `}`
}

func TestDecodeStateRejectsStrictViolations(t *testing.T) {
	bad := strings.Repeat("a", 64)
	cases := map[string]string{
		"empty":                    ``,
		"not an object":            `[]`,
		"malformed":                `{"version":1,`,
		"trailing data":            stateDoc(``) + ` {}`,
		"unknown field":            stateDoc(`,"bogus":1`),
		"duplicate field":          `{"version":1,"version":2,"identity":"` + bad + `","remoteGeneration":1,"baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false}`,
		"null version":             `{"version":null,"identity":"` + bad + `","remoteGeneration":1,"baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false}`,
		"missing version":          `{"identity":"` + bad + `","remoteGeneration":1,"baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false}`,
		"version as string":        `{"version":"1","identity":"` + bad + `","remoteGeneration":1,"baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false}`,
		"missing identity":         `{"version":1,"remoteGeneration":1,"baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false}`,
		"null identity":            `{"version":1,"identity":null,"remoteGeneration":1,"baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false}`,
		"generation as string":     `{"version":1,"identity":"` + bad + `","remoteGeneration":"1","baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false}`,
		"generation negative":      `{"version":1,"identity":"` + bad + `","remoteGeneration":-1,"baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false}`,
		"missing baselineHead":     `{"version":1,"identity":"` + bad + `","remoteGeneration":1,"baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false}`,
		"missing baselineTree":     `{"version":1,"identity":"` + bad + `","remoteGeneration":1,"baselineHead":"` + strings.Repeat("b", 40) + `","recoveryRequired":false}`,
		"missing recoveryRequired": `{"version":1,"identity":"` + bad + `","remoteGeneration":1,"baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `}`,
		"recovery as number":       `{"version":1,"identity":"` + bad + `","remoteGeneration":1,"baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":1}`,
		"boolean identity":         `{"version":1,"identity":true,"remoteGeneration":1,"baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false}`,
		"generation overflow":      `{"version":1,"identity":"` + bad + `","remoteGeneration":18446744073709551616,"baselineHead":"` + strings.Repeat("b", 40) + `","baselineTree":"` + EmptyTreeID.String() + `","recoveryRequired":false}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeState([]byte(doc)); err == nil {
				t.Fatalf("decodeState() succeeded for %s", name)
			}
		})
	}
}

func TestPersistStateIsAtomic(t *testing.T) {
	dir := t.TempDir()
	st := fixtureState()
	got, err := persistState(dir, st.Identity, st)
	if err != nil {
		t.Fatalf("persistState() = %v", err)
	}
	if got != st {
		t.Fatalf("persistState() returned %+v, want %+v", got, st)
	}
	if _, err := os.Stat(filepath.Join(dir, stateFileName)); err != nil {
		t.Fatalf("state.json missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, stateTmpName)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("state.json.tmp left behind: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, stateFileName))
	if err != nil {
		t.Fatalf("stat state.json: %v", err)
	}
	if info.Mode()&0o777 != 0o600 {
		t.Fatalf("state.json mode = %o, want 0600", info.Mode()&0o777)
	}
	back, err := readStateFile(dir)
	if err != nil {
		t.Fatalf("readStateFile() = %v", err)
	}
	if back != st {
		t.Fatalf("readStateFile() = %+v, want %+v", back, st)
	}
}

func TestPersistStateStampsIdentity(t *testing.T) {
	dir := t.TempDir()
	st := fixtureState()
	st.Identity = ""
	if _, err := persistState(dir, strings.Repeat("a", 64), st); err != nil {
		t.Fatalf("persistState() = %v", err)
	}
	back, err := readStateFile(dir)
	if err != nil {
		t.Fatalf("readStateFile() = %v", err)
	}
	if back.Identity != strings.Repeat("a", 64) {
		t.Fatalf("persistState() did not stamp the derived key: %q", back.Identity)
	}
}

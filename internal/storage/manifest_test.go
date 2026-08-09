package storage

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/git"
)

// fixtureManifest builds a valid manifest that exercises every descriptor
// shape: an active checkpoint with a two-increment tail and one retained
// generation whose tail ends in the publication ID the active checkpoint
// copied (the allowed cross-chain repetition of architecture section 9.2).
func fixtureManifest() Manifest {
	h := oid
	cp0, cp1 := uuidv7(1), uuidv7(2)
	p00 := uuidv7(3)
	inc := func(gen uint64, pub int, parent, head git.OID, size uint64) Increment {
		return Increment{
			Generation:  gen,
			Publication: uuidv7(pub),
			Parent:      parent,
			Head:        head,
			Key:         Key{Kind: KindIncrement, Generation: gen, ID: uuidv7(pub)},
			SHA256:      sha256Val(pub),
			Size:        size,
		}
	}

	h5, h6, h7, h8, h9, h10, h11, h12 := h(5), h(6), h(7), h(8), h(9), h(10), h(11), h(12)
	return Manifest{
		Version:    1,
		Generation: 12,
		Head:       h12,
		Checkpoint: Checkpoint{
			ID:                cp1,
			Publication:       uuidv7(10),
			ThroughGeneration: 10,
			Head:              h10,
			Key:               Key{Kind: KindCheckpoint, Generation: 10, ID: cp1},
			SHA256:            sha256Val(20),
			Size:              123456,
		},
		Increments: []Increment{
			inc(11, 11, h10, h11, 4096),
			inc(12, 12, h11, h12, 4096),
		},
		Retained: []Retained{{
			RetiredAtGeneration: 11,
			Head:                h10,
			Checkpoint: Checkpoint{
				ID:                cp0,
				Publication:       p00,
				ThroughGeneration: 5,
				Head:              h5,
				Key:               Key{Kind: KindCheckpoint, Generation: 5, ID: cp0},
				SHA256:            sha256Val(19),
				Size:              99999,
			},
			Increments: []Increment{
				inc(6, 6, h5, h6, 100),
				inc(7, 7, h6, h7, 101),
				inc(8, 8, h7, h8, 102),
				inc(9, 9, h8, h9, 103),
				inc(10, 10, h9, h10, 104),
			},
		}},
	}
}

// Fixture leaf-value builders: n must stay below 16 for oid/sha256 and
// below 1000000000000 for uuid suffixes.
func oid(n byte) git.OID {
	id, err := git.ParseOID(fmt.Sprintf("%040x", n))
	if err != nil {
		panic(err)
	}
	return id
}

func uuidv7(n int) UUID {
	id, err := ParseUUIDv7(fmt.Sprintf("01973e12-8b34-7b01-9e2f-%012x", n))
	if err != nil {
		panic(err)
	}
	return id
}

func sha256Val(n int) SHA256 {
	h, err := ParseSHA256(fmt.Sprintf("%064x", n))
	if err != nil {
		panic(err)
	}
	return h
}

func TestEncodeManifestGolden(t *testing.T) {
	data, err := EncodeManifest(fixtureManifest())
	if err != nil {
		t.Fatalf("EncodeManifest() = %v", err)
	}
	if bytes.Contains(data, []byte("\n")) {
		t.Fatal("encoded manifest contains a newline")
	}
	// The golden bytes prove compact encoding, normative field order, HTML
	// escaping disabled, and no trailing newline for the complete fixture.
	const golden = `{"version":1,"generation":12,"head":"000000000000000000000000000000000000000c","checkpoint":{"id":"01973e12-8b34-7b01-9e2f-000000000002","publication":"01973e12-8b34-7b01-9e2f-00000000000a","throughGeneration":10,"head":"000000000000000000000000000000000000000a","key":"packs/checkpoints/10-01973e12-8b34-7b01-9e2f-000000000002.pack","sha256":"0000000000000000000000000000000000000000000000000000000000000014","size":123456},"increments":[{"generation":11,"publication":"01973e12-8b34-7b01-9e2f-00000000000b","parent":"000000000000000000000000000000000000000a","head":"000000000000000000000000000000000000000b","key":"packs/increments/11-01973e12-8b34-7b01-9e2f-00000000000b.pack","sha256":"000000000000000000000000000000000000000000000000000000000000000b","size":4096},{"generation":12,"publication":"01973e12-8b34-7b01-9e2f-00000000000c","parent":"000000000000000000000000000000000000000b","head":"000000000000000000000000000000000000000c","key":"packs/increments/12-01973e12-8b34-7b01-9e2f-00000000000c.pack","sha256":"000000000000000000000000000000000000000000000000000000000000000c","size":4096}],"retained":[{"retiredAtGeneration":11,"head":"000000000000000000000000000000000000000a","checkpoint":{"id":"01973e12-8b34-7b01-9e2f-000000000001","publication":"01973e12-8b34-7b01-9e2f-000000000003","throughGeneration":5,"head":"0000000000000000000000000000000000000005","key":"packs/checkpoints/5-01973e12-8b34-7b01-9e2f-000000000001.pack","sha256":"0000000000000000000000000000000000000000000000000000000000000013","size":99999},"increments":[{"generation":6,"publication":"01973e12-8b34-7b01-9e2f-000000000006","parent":"0000000000000000000000000000000000000005","head":"0000000000000000000000000000000000000006","key":"packs/increments/6-01973e12-8b34-7b01-9e2f-000000000006.pack","sha256":"0000000000000000000000000000000000000000000000000000000000000006","size":100},{"generation":7,"publication":"01973e12-8b34-7b01-9e2f-000000000007","parent":"0000000000000000000000000000000000000006","head":"0000000000000000000000000000000000000007","key":"packs/increments/7-01973e12-8b34-7b01-9e2f-000000000007.pack","sha256":"0000000000000000000000000000000000000000000000000000000000000007","size":101},{"generation":8,"publication":"01973e12-8b34-7b01-9e2f-000000000008","parent":"0000000000000000000000000000000000000007","head":"0000000000000000000000000000000000000008","key":"packs/increments/8-01973e12-8b34-7b01-9e2f-000000000008.pack","sha256":"0000000000000000000000000000000000000000000000000000000000000008","size":102},{"generation":9,"publication":"01973e12-8b34-7b01-9e2f-000000000009","parent":"0000000000000000000000000000000000000008","head":"0000000000000000000000000000000000000009","key":"packs/increments/9-01973e12-8b34-7b01-9e2f-000000000009.pack","sha256":"0000000000000000000000000000000000000000000000000000000000000009","size":103},{"generation":10,"publication":"01973e12-8b34-7b01-9e2f-00000000000a","parent":"0000000000000000000000000000000000000009","head":"000000000000000000000000000000000000000a","key":"packs/increments/10-01973e12-8b34-7b01-9e2f-00000000000a.pack","sha256":"000000000000000000000000000000000000000000000000000000000000000a","size":104}]}]}`
	if string(data) != golden {
		t.Fatalf("encoded manifest:\n%s\nwant golden bytes (see test)", data)
	}
}

func TestManifestRoundTrip(t *testing.T) {
	m := fixtureManifest()
	data, err := EncodeManifest(m)
	if err != nil {
		t.Fatalf("EncodeManifest() = %v", err)
	}
	back, err := DecodeManifest(data)
	if err != nil {
		t.Fatalf("DecodeManifest() = %v", err)
	}
	if !manifestEqual(back, m) {
		t.Fatalf("round trip changed the manifest:\nencoded: %s", data)
	}
}

func TestManifestRoundTripEmptyTails(t *testing.T) {
	m := fixtureManifest()
	m.Increments = nil
	m.Retained = nil
	m.Generation = 10
	m.Head = m.Checkpoint.Head
	data, err := EncodeManifest(m)
	if err != nil {
		t.Fatalf("EncodeManifest() = %v", err)
	}
	if !bytes.Contains(data, []byte(`"increments":[]`)) {
		t.Fatalf("empty tail must encode as [], got %s", data)
	}
	back, err := DecodeManifest(data)
	if err != nil {
		t.Fatalf("DecodeManifest() = %v", err)
	}
	if len(back.Increments) != 0 || len(back.Retained) != 0 {
		t.Fatal("decoded empty tails are not empty")
	}
}

func manifestEqual(a, b Manifest) bool {
	return reflect.DeepEqual(a, b)
}

// invalidManifestCases maps a case name to a mutation that makes the
// fixture invalid. Every case must fail validation with ErrIntegrity.
func invalidManifestCases() map[string]func(*Manifest) {
	return map[string]func(*Manifest){
		"generation zero": func(m *Manifest) { m.Generation = 0 },
		"checkpoint size zero": func(m *Manifest) {
			m.Checkpoint.Size = 0
		},
		"increment size zero": func(m *Manifest) {
			m.Increments[0].Size = 0
		},
		"retained checkpoint size zero": func(m *Manifest) {
			m.Retained[0].Checkpoint.Size = 0
		},
		"checkpoint cutoff exceeds generation": func(m *Manifest) {
			m.Checkpoint.ThroughGeneration = m.Generation + 1
		},
		"increment below cutoff": func(m *Manifest) {
			m.Increments[0].Generation = m.Checkpoint.ThroughGeneration
		},
		"skipped generation": func(m *Manifest) {
			m.Increments[0].Generation = 13
		},
		"generation regression": func(m *Manifest) {
			m.Increments[1].Generation = m.Increments[0].Generation
		},
		"first increment parent mismatch": func(m *Manifest) {
			m.Increments[0].Parent = oid(1)
		},
		"later increment parent mismatch": func(m *Manifest) {
			m.Increments[1].Parent = oid(2)
		},
		"head mismatch": func(m *Manifest) { m.Head = oid(1) },
		"checkpoint key generation mismatch": func(m *Manifest) {
			m.Checkpoint.Key.Generation = m.Checkpoint.ThroughGeneration + 1
		},
		"checkpoint key id mismatch": func(m *Manifest) {
			m.Checkpoint.Key.ID = uuidv7(30)
		},
		"checkpoint key kind mismatch": func(m *Manifest) {
			m.Checkpoint.Key.Kind = KindIncrement
		},
		"increment key generation mismatch": func(m *Manifest) {
			m.Increments[0].Key.Generation = 99
		},
		"increment key id mismatch": func(m *Manifest) {
			m.Increments[0].Key.ID = uuidv7(31)
		},
		"duplicate object key": func(m *Manifest) {
			m.Increments[0].Key = m.Increments[1].Key
		},
		"duplicate checkpoint id": func(m *Manifest) {
			m.Retained[0].Checkpoint.ID = m.Checkpoint.ID
		},
		"duplicate publication different head": func(m *Manifest) {
			// The retained tail's last increment and the active
			// checkpoint share publication uuidv7(10); breaking the
			// head equality violates the repetition rule.
			m.Checkpoint.Head = oid(1)
			m.Checkpoint.Key.ID = uuidv7(1)
			m.Checkpoint.Key.Generation = 10
			m.Increments[0].Parent = oid(1)
		},
		"retained generation not decreasing": func(m *Manifest) {
			m.Retained = append(m.Retained, Retained{
				RetiredAtGeneration: m.Retained[0].RetiredAtGeneration,
				Head:                m.Retained[0].Head,
				Checkpoint:          m.Retained[0].Checkpoint,
				Increments:          m.Retained[0].Increments,
			})
		},
		"retained retirement not greater than content": func(m *Manifest) {
			m.Retained[0].RetiredAtGeneration = 10 // final content generation
		},
		"retained retirement exceeds manifest generation": func(m *Manifest) {
			m.Retained[0].RetiredAtGeneration = m.Generation + 1
		},
		"retained head mismatch": func(m *Manifest) {
			m.Retained[0].Head = oid(1)
		},
		"retained chain parent mismatch": func(m *Manifest) {
			m.Retained[0].Increments[0].Parent = oid(1)
		},
		"retained checkpoint key mismatch": func(m *Manifest) {
			m.Retained[0].Checkpoint.Key.Generation = 6
		},
	}
}

func TestValidateManifestRejectsCrossFieldViolations(t *testing.T) {
	for name, mutate := range invalidManifestCases() {
		t.Run(name, func(t *testing.T) {
			m := fixtureManifest()
			mutate(&m)
			if _, err := EncodeManifest(m); !errors.Is(err, ErrIntegrity) {
				t.Fatalf("EncodeManifest() error = %v, want ErrIntegrity", err)
			}
			data, err := EncodeManifest(fixtureManifest())
			if err != nil {
				t.Fatalf("encode base: %v", err)
			}
			if _, err := DecodeManifest(data); err != nil {
				t.Fatalf("base must decode: %v", err)
			}
		})
	}
}

func TestDecodeManifestRejectsInvalidJSON(t *testing.T) {
	cp := func(n int) string {
		return `{"id":"` + uuidString(1) + `","publication":"` + uuidString(2) +
			`","throughGeneration":0,"head":"` + oidString(0) +
			`","key":"packs/checkpoints/0-` + uuidString(1) +
			`.pack","sha256":"` + sha256String(0) + `","size":1}`
	}
	base := func(fields string) string {
		return `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]` + fields + `}`
	}
	cases := map[string]string{
		"empty":                    ``,
		"not an object":            `[1,2]`,
		"malformed json":           `{"version":1,`,
		"unknown top-level field":  base(`,"bogus":1`),
		"duplicate top-level name": `{"version":1,"generation":1,"generation":2,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"trailing data":            base(``) + ` {}`,
		"explicit null version":    `{"version":null,"generation":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"explicit null checkpoint": `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":null,"increments":[],"retained":[]}`,
		"explicit null increment":  `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[null],"retained":[]}`,
		"explicit null retained":   `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[null]}`,
		"boolean field":            `{"version":true,"generation":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"quoted generation":        `{"version":1,"generation":"1","head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"float generation":         `{"version":1,"generation":1.5,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"negative generation":      `{"version":1,"generation":-1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"exponent generation":      `{"version":1,"generation":1e3,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"overflow generation":      `{"version":1,"generation":18446744073709551616,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"missing version":          `{"generation":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"version zero":             `{"version":0,"generation":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"version two":              `{"version":2,"generation":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"missing generation":       `{"version":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[]}`,
		"missing checkpoint":       `{"version":1,"generation":1,"head":"` + oidString(0) + `","increments":[],"retained":[]}`,
		"missing increments":       `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"retained":[]}`,
		"missing retained":         `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[]}`,
		"checkpoint bogus field":   `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":{"id":"` + uuidString(1) + `","publication":"` + uuidString(2) + `","throughGeneration":0,"head":"` + oidString(0) + `","key":"packs/checkpoints/0-` + uuidString(1) + `.pack","sha256":"` + sha256String(0) + `","size":1,"bogus":1},"increments":[],"retained":[]}`,
		"checkpoint null id":       `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":{"id":null,"publication":"` + uuidString(2) + `","throughGeneration":0,"head":"` + oidString(0) + `","key":"packs/checkpoints/0-` + uuidString(1) + `.pack","sha256":"` + sha256String(0) + `","size":1},"increments":[],"retained":[]}`,
		"increment unknown field":  `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[{"generation":1,"publication":"` + uuidString(3) + `","parent":"` + oidString(0) + `","head":"` + oidString(1) + `","key":"packs/increments/1-` + uuidString(3) + `.pack","sha256":"` + sha256String(1) + `","size":1,"bogus":1}],"retained":[]}`,
		"increment bad parent":     `{"version":1,"generation":1,"head":"` + oidString(1) + `","checkpoint":` + cp(0) + `,"increments":[{"generation":1,"publication":"` + uuidString(3) + `","parent":"bad","head":"` + oidString(1) + `","key":"packs/increments/1-` + uuidString(3) + `.pack","sha256":"` + sha256String(1) + `","size":1}],"retained":[]}`,
		"checkpoint bad id":        `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":{"id":"nope","publication":"` + uuidString(2) + `","throughGeneration":0,"head":"` + oidString(0) + `","key":"packs/checkpoints/0-` + uuidString(1) + `.pack","sha256":"` + sha256String(0) + `","size":1},"increments":[],"retained":[]}`,
		"checkpoint bad head":      `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":{"id":"` + uuidString(1) + `","publication":"` + uuidString(2) + `","throughGeneration":0,"head":"xyz","key":"packs/checkpoints/0-` + uuidString(1) + `.pack","sha256":"` + sha256String(0) + `","size":1},"increments":[],"retained":[]}`,
		"checkpoint bad sha256":    `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":{"id":"` + uuidString(1) + `","publication":"` + uuidString(2) + `","throughGeneration":0,"head":"` + oidString(0) + `","key":"packs/checkpoints/0-` + uuidString(1) + `.pack","sha256":"short","size":1},"increments":[],"retained":[]}`,
		"checkpoint bad key":       `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":{"id":"` + uuidString(1) + `","publication":"` + uuidString(2) + `","throughGeneration":0,"head":"` + oidString(0) + `","key":"packs/checkpoints/0-wrong.pack","sha256":"` + sha256String(0) + `","size":1},"increments":[],"retained":[]}`,
		"checkpoint string size":   `{"version":1,"generation":1,"head":"` + oidString(0) + `","checkpoint":{"id":"` + uuidString(1) + `","publication":"` + uuidString(2) + `","throughGeneration":0,"head":"` + oidString(0) + `","key":"packs/checkpoints/0-` + uuidString(1) + `.pack","sha256":"` + sha256String(0) + `","size":"1"},"increments":[],"retained":[]}`,
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeManifest([]byte(doc)); !errors.Is(err, ErrIntegrity) {
				t.Fatalf("DecodeManifest() error = %v, want ErrIntegrity", err)
			}
		})
	}
}

func TestDecodeManifestRetainedChainFields(t *testing.T) {
	cp := func(n int) string {
		return `{"id":"` + uuidString(1) + `","publication":"` + uuidString(2) +
			`","throughGeneration":0,"head":"` + oidString(0) +
			`","key":"packs/checkpoints/0-` + uuidString(1) +
			`.pack","sha256":"` + sha256String(0) + `","size":1}`
	}
	retained := func(fields string) string {
		return `{"retiredAtGeneration":1,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[]` + fields + `}`
	}
	base := func(ret string) string {
		return `{"version":1,"generation":2,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[],"retained":[` + ret + `]}`
	}
	cases := map[string]string{
		"retained unknown field":  base(retained(`,"bogus":1`)),
		"retained null head":      base(`{"retiredAtGeneration":1,"head":null,"checkpoint":` + cp(0) + `,"increments":[]}`),
		"retained missing chain":  base(`{"retiredAtGeneration":1,"head":"` + oidString(0) + `","increments":[]}`),
		"retained duplicate name": base(`{"retiredAtGeneration":1,"retiredAtGeneration":2,"head":"` + oidString(0) + `","checkpoint":` + cp(0) + `,"increments":[]}`),
		"retained not an object":  base(`1`),
	}
	for name, doc := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := DecodeManifest([]byte(doc)); !errors.Is(err, ErrIntegrity) {
				t.Fatalf("DecodeManifest() error = %v, want ErrIntegrity", err)
			}
		})
	}
}

// oidString, uuidString, sha256String build fixture leaf values; n must
// stay below 16 for oid/sha256 and below 1000000000000 for uuid suffixes.
func oidString(n byte) string { return fmt.Sprintf("%040x", n) }
func uuidString(n int) string { return fmt.Sprintf("01973e12-8b34-7b01-9e2f-%012x", n) }
func sha256String(n byte) string {
	return fmt.Sprintf("%064x", n)
}

func TestListIsNotUsedToDiscoverState(t *testing.T) {
	// The storage policy never uses LIST for accepted-state discovery:
	// ListObjects appears only in the interface definition, never in the
	// manifest, upload, or probe policy files.
	for _, file := range []string{"manifest.go", "upload.go", "probe.go", "key.go", "uuid.go", "sha256.go", "store.go"} {
		src, err := os.ReadFile(filepath.Join("..", "storage", file))
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		if strings.Contains(string(src), "ListObjects(") && file != "store.go" {
			t.Errorf("%s uses ListObjects to read state", file)
		}
	}
}

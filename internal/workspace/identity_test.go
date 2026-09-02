package workspace

import (
	"strings"
	"testing"
)

func testIdentity() Identity {
	return Identity{
		Endpoint:        "https://s3.example.com",
		Region:          "eu-central-1",
		Bucket:          "notes",
		Prefix:          "slivingdoc",
		ManifestVersion: ManifestVersion,
	}
}

func TestDerivedKeyDeterministic(t *testing.T) {
	first := DerivedKey("/workspace/notes", testIdentity())
	if len(first) != 64 {
		t.Fatalf("DerivedKey() length = %d, want 64", len(first))
	}
	for range 5 {
		if got := DerivedKey("/workspace/notes", testIdentity()); got != first {
			t.Fatalf("DerivedKey() = %q, want stable %q", got, first)
		}
	}
	for _, c := range first {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("DerivedKey() %q contains non-lowercase-hex %q", first, c)
		}
	}
}

func TestDerivedKeyDistinguishesInputs(t *testing.T) {
	base := testIdentity()
	cases := map[string]Identity{
		"endpoint":  {Endpoint: "https://other.example.com", Region: base.Region, Bucket: base.Bucket, Prefix: base.Prefix, ManifestVersion: base.ManifestVersion},
		"region":    {Endpoint: base.Endpoint, Region: "us-west-2", Bucket: base.Bucket, Prefix: base.Prefix, ManifestVersion: base.ManifestVersion},
		"bucket":    {Endpoint: base.Endpoint, Region: base.Region, Bucket: "other", Prefix: base.Prefix, ManifestVersion: base.ManifestVersion},
		"prefix":    {Endpoint: base.Endpoint, Region: base.Region, Bucket: base.Bucket, Prefix: "other", ManifestVersion: base.ManifestVersion},
		"version":   {Endpoint: base.Endpoint, Region: base.Region, Bucket: base.Bucket, Prefix: base.Prefix, ManifestVersion: 2},
		"no bucket": {Endpoint: base.Endpoint, Region: base.Region, Prefix: base.Prefix, ManifestVersion: base.ManifestVersion},
	}
	want := DerivedKey("/workspace/notes", base)
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			if got := DerivedKey("/workspace/notes", id); got == want {
				t.Fatalf("DerivedKey() equal for differing identity")
			}
		})
	}
	for _, p := range []string{"/workspace/other", "/other/notes"} {
		if got := DerivedKey(p, base); got == want {
			t.Fatalf("DerivedKey(%q) equal to the reference path", p)
		}
	}
}

func TestDerivedKeyLengthPrefixed(t *testing.T) {
	// A naive concatenation makes ("/a", "b") and ("/ab", "") collide.
	// Length prefixes keep the encodings unambiguous.
	id := func(endpoint string) Identity {
		return Identity{Endpoint: endpoint, Region: "r", Bucket: "b", Prefix: "p", ManifestVersion: 1}
	}
	a := DerivedKey("/a", id("b"))
	b := DerivedKey("/ab", id(""))
	if a == b {
		t.Fatal("DerivedKey() collides across boundary shifts")
	}
}

func TestSharedCacheDirNameShape(t *testing.T) {
	first := SharedCacheDirName(testIdentity())
	if first != "notes-slivingdoc-"+first[len(first)-16:] {
		t.Fatalf("SharedCacheDirName() = %q, want the sanitized bucket and prefix followed by a 16-hex digest", first)
	}
	for range 5 {
		if got := SharedCacheDirName(testIdentity()); got != first {
			t.Fatalf("SharedCacheDirName() = %q, want stable %q", got, first)
		}
	}
	for _, c := range first[len(first)-16:] {
		if !strings.ContainsRune("0123456789abcdef", c) {
			t.Fatalf("SharedCacheDirName() digest of %q contains non-lowercase-hex %q", first, c)
		}
	}
}

func TestSharedCacheDirNameDistinguishesIdentities(t *testing.T) {
	base := testIdentity()
	cases := map[string]Identity{
		"endpoint": {Endpoint: "https://other.example.com", Region: base.Region, Bucket: base.Bucket, Prefix: base.Prefix, ManifestVersion: base.ManifestVersion},
		"region":   {Endpoint: base.Endpoint, Region: "us-west-2", Bucket: base.Bucket, Prefix: base.Prefix, ManifestVersion: base.ManifestVersion},
		"bucket":   {Endpoint: base.Endpoint, Region: base.Region, Bucket: "other", Prefix: base.Prefix, ManifestVersion: base.ManifestVersion},
		"prefix":   {Endpoint: base.Endpoint, Region: base.Region, Bucket: base.Bucket, Prefix: "other", ManifestVersion: base.ManifestVersion},
		"version":  {Endpoint: base.Endpoint, Region: base.Region, Bucket: base.Bucket, Prefix: base.Prefix, ManifestVersion: 2},
	}
	want := SharedCacheDirName(base)
	for name, id := range cases {
		t.Run(name, func(t *testing.T) {
			if got := SharedCacheDirName(id); got == want {
				t.Fatalf("SharedCacheDirName() equal for differing identity")
			}
		})
	}
}

func TestSharedCacheDirNameSanitizesComponents(t *testing.T) {
	id := testIdentity()
	id.Bucket = "My.Bucket"
	id.Prefix = "team/notes"
	got := SharedCacheDirName(id)
	if !strings.HasPrefix(got, "my-bucket-team-notes-") {
		t.Fatalf("SharedCacheDirName() = %q, want sanitized lowercase components", got)
	}
	if strings.ContainsAny(got, "/.") {
		t.Fatalf("SharedCacheDirName() = %q contains filesystem-hostile characters", got)
	}
	id.Prefix = strings.Repeat("p", 100)
	if long := SharedCacheDirName(id); len(long) > 2*sanitizeCacheComponentMax+sharedCacheDigestLen+2 {
		t.Fatalf("SharedCacheDirName() length = %d, want each component bounded", len(long))
	}
}

func TestDerivedKeyHidesVisiblePath(t *testing.T) {
	key := DerivedKey("/workspace/secret-topic/agents", testIdentity())
	for _, frag := range []string{"secret-topic", "agents", "workspace", "notes"} {
		if strings.Contains(key, frag) {
			t.Fatalf("DerivedKey() %q exposes the path fragment %q", key, frag)
		}
	}
}

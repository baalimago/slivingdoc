package sitelogin

import (
	"context"
	"encoding/json"
	"errors"
	"maps"
	"net/http"
	"strings"
	"testing"

	"github.com/baalimago/slivingdoc/internal/credentials"
	"github.com/baalimago/slivingdoc/internal/sitelogin/sitetest"
)

func keyedSite(t *testing.T, access string) (*sitetest.Site, *Client) {
	t.Helper()
	site := sitetest.Start(t)
	site.SetSpaces(testToken,
		sitetest.Space{Name: "notes", Owner: "ada@example.test", Access: "write"},
		sitetest.Space{Name: "team", Owner: "bob@example.test", Access: "read"},
	)
	is := issue()
	is.Access = access
	site.Next(sitetest.Script{Issue: is})
	client, _ := newClient(t, site.URL())
	a, err := client.Start(context.Background(), StartRequest{Access: credentials.Access(access)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.Wait(context.Background(), a); err != nil {
		t.Fatalf("Wait() = %v", err)
	}
	return site, client
}

func TestSpaces(t *testing.T) {
	site, client := keyedSite(t, "write")
	got, err := client.Spaces(context.Background(), testToken)
	if err != nil {
		t.Fatalf("Spaces() = %v", err)
	}
	want := []Space{
		{Name: "notes", Owner: "ada@example.test", Access: credentials.AccessWrite},
		{Name: "team", Owner: "bob@example.test", Access: credentials.AccessRead},
	}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("Spaces() = %+v, want %+v", got, want)
	}
	site.Suspend("team")
	if got, _ := client.Spaces(context.Background(), testToken); len(got) != 1 {
		t.Fatalf("Spaces() with a suspended owner = %+v, want it left out", got)
	}
	site.Revoke(testToken)
	_, err = client.Spaces(context.Background(), testToken)
	var refusal *Refusal
	if !errors.As(err, &refusal) || refusal.Status != http.StatusUnauthorized || refusal.Code != "invalid_token" {
		t.Fatalf("Spaces() with a revoked key = %v, want 401 invalid_token", err)
	}
	if _, err := client.Spaces(context.Background(), "sld bad"); err == nil || strings.Contains(err.Error(), "sld bad") {
		t.Fatalf("Spaces() with an unsendable key = %v, want a refusal without it", err)
	}
}

func TestSpacesCapsAReadKey(t *testing.T) {
	_, client := keyedSite(t, "read")
	got, err := client.Spaces(context.Background(), testToken)
	if err != nil || len(got) != 2 || got[0].Access != credentials.AccessRead {
		t.Fatalf("Spaces() of a read key = %+v, %v; want every space read only", got, err)
	}
}

func TestSpacesRefusesAnswersOutsideTheContract(t *testing.T) {
	for name, body := range map[string]string{
		"no list":              `{}`,
		"null list":            `{"spaces":null}`,
		"not json":             `{`,
		"bad name":             `{"spaces":[{"name":"No_Space","owner":"a@example.test","access":"write"}]}`,
		"no owner":             `{"spaces":[{"name":"notes","owner":"","access":"write"}]}`,
		"bad access":           `{"spaces":[{"name":"notes","owner":"a@example.test","access":"admin"}]}`,
		"listed twice":         `{"spaces":[{"name":"notes","owner":"a@example.test","access":"write"},{"name":"notes","owner":"a@example.test","access":"read"}]}`,
		"owner with an escape": `{"spaces":[{"name":"notes","owner":"a\u001b[2J@example.test","access":"write"}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			client, _ := newClient(t, answer(t, http.StatusOK, body))
			if _, err := client.Spaces(context.Background(), testToken); !errors.Is(err, ErrProtocol) {
				t.Fatalf("Spaces() = %v, want ErrProtocol", err)
			}
		})
	}
	empty, _ := newClient(t, answer(t, http.StatusOK, `{"spaces":[]}`))
	if got, err := empty.Spaces(context.Background(), testToken); err != nil || len(got) != 0 {
		t.Fatalf("Spaces() of an account without spaces = %v, %v; want an empty list", got, err)
	}
	big, _ := newClient(t, answer(t, http.StatusOK, `{"spaces":[`+strings.Repeat(" ", spacesLimit)+`]}`))
	if _, err := big.Spaces(context.Background(), testToken); !errors.Is(err, ErrProtocol) || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("Spaces() of an oversize answer = %v, want ErrProtocol", err)
	}
}

func TestMint(t *testing.T) {
	site, client := keyedSite(t, "write")
	var hooked []sitetest.Minted
	site.OnMint(func(m sitetest.Minted) { hooked = append(hooked, m) })
	got, err := client.Mint(context.Background(), testToken, "notes", "")
	if err != nil {
		t.Fatalf("Mint() = %v", err)
	}
	if got.Space != "notes" || got.Access != credentials.AccessWrite || got.Endpoint != endpoint || got.Owner != "ada@example.test" ||
		got.Expires.Never() || got.Token == testToken || len(hooked) != 1 || hooked[0].Token != got.Token {
		t.Fatalf("Mint() = %+v, want a write token of notes (hooked %+v)", got, hooked)
	}
	read, err := client.Mint(context.Background(), testToken, "notes", credentials.AccessRead)
	if err != nil || read.Access != credentials.AccessRead {
		t.Fatalf("Mint(read) = %+v, %v; want a read token", read, err)
	}
	if mints := site.Mints(); len(mints) != 2 || mints[1].Key != testToken {
		t.Fatalf("mints = %+v", mints)
	}
	var refusal *Refusal
	for _, tt := range []struct {
		name   string
		setup  func()
		space  string
		access credentials.Access
		status int
		code   string
	}{
		{"unknown space", func() {}, "other", "", http.StatusNotFound, "no_space"},
		{"write on a read grant", func() {}, "team", credentials.AccessWrite, http.StatusForbidden, "access_denied"},
		{"busy", func() { site.RefuseMint(http.StatusTooManyRequests, "busy") }, "notes", "", http.StatusTooManyRequests, "busy"},
		{"suspended", func() { site.Suspend("notes") }, "notes", "", http.StatusForbidden, "space_suspended"},
	} {
		tt.setup()
		_, err := client.Mint(context.Background(), testToken, tt.space, tt.access)
		if !errors.As(err, &refusal) || refusal.Status != tt.status || refusal.Code != tt.code {
			t.Fatalf("%s: Mint() = %v, want HTTP %d %s", tt.name, err, tt.status, tt.code)
		}
	}
	if err := client.Revoke(context.Background(), testToken); err != nil {
		t.Fatalf("Revoke() = %v", err)
	}
	if got := site.Revoked(); len(got) != 3 || got[0] != testToken || got[2] != read.Token {
		t.Fatalf("revoked = %v, want the key and both minted tokens", got)
	}
	_, err = client.Mint(context.Background(), testToken, "team", "")
	if !errors.As(err, &refusal) || refusal.Status != http.StatusUnauthorized {
		t.Fatalf("Mint() with a revoked key = %v, want 401", err)
	}
	if _, err := client.Mint(context.Background(), "sld bad", "notes", ""); err == nil || strings.Contains(err.Error(), "sld bad") {
		t.Fatalf("Mint() with an unsendable key = %v, want a refusal without it", err)
	}
	if _, err := client.Mint(context.Background(), testToken, "No_Space", ""); err == nil {
		t.Fatal("Mint() of an invalid space name = nil, want a refusal before sending")
	}
}

func TestMintRefusesAnswersOutsideTheContract(t *testing.T) {
	const minted = "sld_00000000000000aa_bWludGVkLXRva2VuLWZvci10aGUtbWludC10ZXN0cw"
	good := map[string]string{
		"token": minted, "space": "notes", "access": "read", "expiresAt": "2026-09-27T13:00:00Z",
		"endpoint": endpoint, "owner": "ada@example.test",
	}
	for _, tt := range []struct {
		name, field, value string
		rejected           bool
	}{
		{"unsendable token", "token", "sld bad", false},
		{"another space", "space", "team", true},
		{"write when read was asked", "access", "write", true},
		{"bad access", "access", "admin", true},
		{"plain http endpoint", "endpoint", "http://api.example.test", true},
		{"no owner", "owner", "", true},
		{"bad expiry", "expiresAt", "soon", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			body := map[string]string{}
			maps.Copy(body, good)
			body[tt.field] = tt.value
			client, _ := newClient(t, answer(t, http.StatusOK, jsonOf(t, body)))
			_, err := client.Mint(context.Background(), testToken, "notes", credentials.AccessRead)
			if !errors.Is(err, ErrProtocol) || strings.Contains(err.Error(), minted) || strings.Contains(err.Error(), "sld bad") {
				t.Fatalf("Mint() = %v, want ErrProtocol without the token", err)
			}
			var rejected *RejectedError
			if got := errors.As(err, &rejected); got != tt.rejected || (got && rejected.Credential != minted) {
				t.Fatalf("Mint() = %#v, want the token returned for revocation exactly when it is sendable", err)
			}
		})
	}
	noExpiry, _ := newClient(t, answer(t, http.StatusOK, `{"token":"`+minted+`","space":"notes","access":"write","endpoint":"`+endpoint+`","owner":"ada@example.test"}`))
	if _, err := noExpiry.Mint(context.Background(), testToken, "notes", ""); !errors.Is(err, ErrProtocol) {
		t.Fatalf("Mint() without an expiry = %v, want ErrProtocol", err)
	}
}

func jsonOf(t *testing.T, v any) string {
	t.Helper()
	data, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

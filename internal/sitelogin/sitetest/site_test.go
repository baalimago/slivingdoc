package sitetest

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

// The keys the site tests issue: one writable with an account, one read-only
// without one.
const (
	writeSiteKey = "sld_00000000000000bb_c2l0ZXRlc3QtY3JlYXRlLXNwYWNlLXRva2VuLXRlc3Q"
	readSiteKey  = "sld_00000000000000cc_c2l0ZXRlc3QtcmVhZC1zcGFjZS10b2tlbi10ZXN0"
	siteAPI      = "https://api.example.test"
)

// errorAnswer is the site's error body.
type errorAnswer struct {
	Error   string `json:"error"`
	Message string `json:"message"`
}

// issueKey approves one login through the site's own start and token routes,
// so the key carries the access and account a script names.
func issueKey(t *testing.T, site *Site, key, access, account string) string {
	t.Helper()
	site.Next(Script{Issue: Issue{Key: key, Access: access, Endpoint: siteAPI, Account: account}})
	var start struct {
		DeviceCode string `json:"deviceCode"`
	}
	request(t, site, http.MethodPost, "/cli/v1/start", "", `{"access":"`+access+`"}`, http.StatusOK, &start)
	request(t, site, http.MethodPost, "/cli/v1/token", "", `{"deviceCode":"`+start.DeviceCode+`"}`, http.StatusOK, nil)
	return key
}

// request sends one JSON request to the site and decodes the answer into out
// when it is set.
func request(t *testing.T, site *Site, method, path, key, body string, wantStatus int, out any) {
	t.Helper()
	req, err := http.NewRequest(method, site.URL()+path, strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		t.Fatalf("%s %s: read the answer: %v", method, path, err)
	}
	if resp.StatusCode != wantStatus {
		t.Fatalf("%s %s %s = %d %s, want %d", method, path, body, resp.StatusCode, data, wantStatus)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			t.Fatalf("%s %s: decode %s: %v", method, path, data, err)
		}
	}
}

// listed returns the spaces the key reaches.
func listed(t *testing.T, site *Site, key string) []Space {
	t.Helper()
	var ans struct {
		Spaces []Space `json:"spaces"`
	}
	request(t, site, http.MethodGet, "/cli/v1/spaces", key, "", http.StatusOK, &ans)
	return ans.Spaces
}

func TestSiteCreatesASpace(t *testing.T) {
	site := Start(t)
	key := issueKey(t, site, writeSiteKey, "write", "ada@example.test")
	var ans struct {
		Space Space `json:"space"`
	}
	request(t, site, http.MethodPost, "/cli/v1/spaces", key, `{"name":"team-notes"}`, http.StatusCreated, &ans)
	want := Space{Name: "team-notes", Owner: "ada@example.test", Access: "write"}
	if ans.Space != want {
		t.Fatalf("create answer = %+v, want %+v", ans.Space, want)
	}
	if got := listed(t, site, key); len(got) != 1 || got[0] != want {
		t.Fatalf("spaces = %+v, want the created space", got)
	}
}

func TestSiteRefusesCreate(t *testing.T) {
	site := Start(t)
	key := issueKey(t, site, writeSiteKey, "write", "ada@example.test")
	site.SetSpaces(key, Space{Name: "notes", Owner: "ada@example.test", Access: "write"})
	readKey := issueKey(t, site, readSiteKey, "read", "")

	tests := []struct {
		name   string
		key    string
		body   string
		status int
		code   string
	}{
		{"unknown key", "sld_unknown", `{"name":"team-notes"}`, http.StatusUnauthorized, "invalid_token"},
		{"read-only key", readKey, `{"name":"team-notes"}`, http.StatusForbidden, "access_denied"},
		{"missing name", key, `{}`, http.StatusBadRequest, "invalid_request"},
		{"null name", key, `{"name":null}`, http.StatusBadRequest, "invalid_request"},
		{"non-string name", key, `{"name":5}`, http.StatusBadRequest, "invalid_request"},
		{"unknown field", key, `{"name":"team-notes","owner":"bob"}`, http.StatusBadRequest, "invalid_request"},
		{"short name", key, `{"name":"ab"}`, http.StatusBadRequest, "invalid_name"},
		{"reserved name", key, `{"name":"xn--notes"}`, http.StatusBadRequest, "invalid_name"},
		{"name in use", key, `{"name":"notes"}`, http.StatusConflict, "name_taken"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var ans errorAnswer
			request(t, site, http.MethodPost, "/cli/v1/spaces", tt.key, tt.body, tt.status, &ans)
			if ans.Error != tt.code {
				t.Fatalf("error = %q, want %q", ans.Error, tt.code)
			}
			if got := listed(t, site, key); len(got) != 1 {
				t.Fatalf("spaces = %+v, want only the pre-existing one", got)
			}
		})
	}

	for _, rf := range []struct {
		status int
		code   string
	}{
		{http.StatusTooManyRequests, "busy"},
		{http.StatusServiceUnavailable, "pool_empty"},
		{http.StatusForbidden, "space_limit"},
	} {
		site.RefuseCreate(rf.status, rf.code)
		var ans errorAnswer
		request(t, site, http.MethodPost, "/cli/v1/spaces", key, `{"name":"team-notes"}`, rf.status, &ans)
		if ans.Error != rf.code {
			t.Fatalf("error = %q, want the scripted %q", ans.Error, rf.code)
		}
	}
}

func TestSiteRefusesDuplicateName(t *testing.T) {
	site := Start(t)
	key := issueKey(t, site, writeSiteKey, "write", "ada@example.test")
	var ans errorAnswer
	request(t, site, http.MethodPost, "/cli/v1/spaces", key, `{"name":"notes","name":"other"}`, http.StatusBadRequest, &ans)
	if ans.Error != "invalid_request" {
		t.Fatalf("error = %q, want invalid_request", ans.Error)
	}
	if got := listed(t, site, key); len(got) != 0 {
		t.Fatalf("spaces = %+v, want none: a duplicate member creates nothing", got)
	}
}

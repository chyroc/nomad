package ark

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEnsureEnvironmentReusesConflict(t *testing.T) {
	var listed bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/environments":
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "ResourceConflict", "message": "already exists"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/environments":
			listed = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]any{
					{"id": "env_other", "name": "something-else"},
					{"id": "env_existing", "name": environmentName},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "k", "", WithSTSProvider(func() (string, string, string, bool) { return "", "", "", false }))
	id, err := c.EnsureEnvironment(context.Background(), "")
	if err != nil {
		t.Fatalf("EnsureEnvironment: %v", err)
	}
	if id != "env_existing" {
		t.Fatalf("got %q, want env_existing", id)
	}
	if !listed {
		t.Fatal("expected a list fallback after 409")
	}
}

func TestEnsureEnvironmentConflictNoMatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/environments":
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": "ResourceConflict", "message": "exists"},
			})
		case r.Method == http.MethodGet && r.URL.Path == "/environments":
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "k", "", WithSTSProvider(func() (string, string, string, bool) { return "", "", "", false }))
	if _, err := c.EnsureEnvironment(context.Background(), ""); err == nil {
		t.Fatal("conflict with no matching existing env should error")
	}
}

func TestIsConflictError(t *testing.T) {
	if !isConflictError(assertErr("POST /environments: HTTP 409 ResourceConflict")) {
		t.Fatal("should detect 409")
	}
	if isConflictError(assertErr("HTTP 500 boom")) {
		t.Fatal("should not detect 500")
	}
}

type assertErrString string

func (e assertErrString) Error() string { return string(e) }

func assertErr(s string) error { return assertErrString(s) }

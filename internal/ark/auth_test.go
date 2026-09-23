package ark

import (
	"encoding/base64"
	"encoding/json"
	"net/url"
	"testing"
)

func jsonMarshalString(s string) (string, error) {
	b, err := json.Marshal(s)
	return string(b), err
}

func TestExtractAuthCode(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"raw code", "abc123", "abc123"},
		{"query string", "code=abc123&state=ff", "abc123"},
		{"full url", "https://signin.volcengine.com/authorize/oauth/authorize?code=abc123&state=ff", "abc123"},
		{"base64url", base64.RawURLEncoding.EncodeToString([]byte("code=abc123&state=ff")), "abc123"},
		{"base64 std", base64.StdEncoding.EncodeToString([]byte("code=abc123&state=ff")), "abc123"},
		{"surrounded backticks", "`abc123`", "abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractAuthCode(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got %q, want %q", got, tc.want)
			}
		})
	}
}

func TestParseAccessSTS(t *testing.T) {
	inner := `{"access_key_id":"AKTPx","secret_access_key":"sk1","session_token":"tok1"}`
	sts, err := parseAccessSTS(inner)
	if err != nil {
		t.Fatal(err)
	}
	if sts.AccessKeyID != "AKTPx" || sts.SecretAccessKey != "sk1" || sts.SessionToken != "tok1" {
		t.Fatalf("bad parse: %+v", sts)
	}
	wrapped, _ := jsonMarshalString(inner)
	sts2, err := parseAccessSTS(wrapped)
	if err != nil || sts2.AccessKeyID != "AKTPx" {
		t.Fatalf("wrapped parse failed: %+v %v", sts2, err)
	}
}

func TestParseAccessSTS_MissingFields(t *testing.T) {
	if _, err := parseAccessSTS(`{"access_key_id":"x"}`); err == nil {
		t.Fatal("expected error for incomplete STS")
	}
}

func TestPKCE(t *testing.T) {
	v, err := verifierString()
	if err != nil {
		t.Fatal(err)
	}
	if pkceChallenge(v) == "" {
		t.Fatal("empty challenge")
	}
	if pkceChallenge(v) != pkceChallenge(v) {
		t.Fatal("challenge not deterministic")
	}
}

func TestBeginLoginURL(t *testing.T) {
	l, err := BeginLogin()
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(l.AuthorizeURL)
	if err != nil {
		t.Fatalf("bad url: %v", err)
	}
	q := u.Query()
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" ||
		q.Get("client_id") != "trn:signin:::devtools/cross-device" || q.Get("response_type") != "code" {
		t.Fatalf("bad authorize params: %+v", q)
	}
}

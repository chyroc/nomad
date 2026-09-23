// Package auth implements the first-run login and credential cache.
//
// Login uses Volcengine's public cross-device OAuth 2.0 + PKCE flow:
// nomad prints an authorization URL, the user approves it in a browser
// and pastes back the displayed code, which is exchanged at the token
// endpoint. The returned access_token is a JSON document containing a
// short-lived STS triple (ak/sk/session token); nomad then calls the Ark
// OpenAPI CreateApiKey action with a V4-signed request to mint a
// long-lived Ark API key, which is cached locally and used for all
// subsequent runtime calls. The refresh_token can mint fresh STS
// credentials whenever another signed call is needed.
package ark

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	signinBase    = "https://signin.volcengine.com"
	authorizePath = "/authorize/oauth/authorize"
	tokenPath     = "/authorize/oauth/token"
	crossDeviceID = "trn:signin:::devtools/cross-device"
	consoleScope  = "Console:All:All"
	tokenTimeout  = 30 * time.Second
	stateBytes    = 16
	verifierBytes = 32
	stsSkew       = 60 * time.Second
)

// Credentials is the cached credential file (~/.nomad/auth.json, 0600).
type Credentials struct {
	APIKey      string `json:"api_key"`
	AccountID   string `json:"account_id,omitempty"`
	UserID      string `json:"user_id,omitempty"`
	ProjectName string `json:"project_name,omitempty"`

	RefreshToken string    `json:"refresh_token,omitempty"`
	ClientID     string    `json:"client_id,omitempty"`
	Scope        string    `json:"scope,omitempty"`
	AK           string    `json:"ak,omitempty"`
	SK           string    `json:"sk,omitempty"`
	SessionToken string    `json:"session_token,omitempty"`
	STSSource    string    `json:"sts_source,omitempty"` // oauth | arkcli
	STSExpiresAt time.Time `json:"sts_expires_at,omitempty"`
}

// STS returns the current short-lived security triple, refreshing it
// through the OAuth refresh_token grant when expired.
func (c *Credentials) STS(ctx context.Context) (ak, sk, token string, err error) {
	if c.AK != "" && c.SK != "" && c.SessionToken != "" && time.Now().Before(c.STSExpiresAt.Add(-stsSkew)) {
		return c.AK, c.SK, c.SessionToken, nil
	}
	if strings.TrimSpace(c.RefreshToken) == "" {
		return "", "", "", errors.New("auth: no refresh token; please run `nomad login` again")
	}
	tok, err := refreshOAuthToken(ctx, http.DefaultClient, c.ClientID, c.Scope, c.RefreshToken)
	if err != nil {
		return "", "", "", err
	}
	sts, err := parseAccessSTS(tok.AccessToken)
	if err != nil {
		return "", "", "", err
	}
	c.AK = sts.AccessKeyID
	c.SK = sts.SecretAccessKey
	c.SessionToken = sts.SessionToken
	c.STSExpiresAt = time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second)
	if tok.RefreshToken != "" {
		c.RefreshToken = tok.RefreshToken
	}
	return c.AK, c.SK, c.SessionToken, nil
}

// stsTriple is the JSON document carried inside the OAuth access_token.
type stsTriple struct {
	AccessKeyID     string     `json:"access_key_id"`
	SecretAccessKey string     `json:"secret_access_key"`
	SessionToken    string     `json:"session_token"`
	Data            *stsTriple `json:"data,omitempty"`
}

// oauthTokenResponse is the signin token endpoint success payload.
type oauthTokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	ExpiresIn    int    `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
	IDToken      string `json:"id_token"`
}

// LoginResult is what the interactive flow produces.
type LoginResult struct {
	AuthorizeURL string
	state        string
	verifier     string
}

// BeginLogin generates the cross-device authorization URL and the PKCE
// parameters needed to complete the exchange.
func BeginLogin() (*LoginResult, error) {
	state, err := randomHex(stateBytes)
	if err != nil {
		return nil, err
	}
	verifier, err := verifierString()
	if err != nil {
		return nil, err
	}
	challenge := pkceChallenge(verifier)

	q := url.Values{}
	q.Set("client_id", crossDeviceID)
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("redirect_uri", signinBase+authorizePath)
	q.Set("response_type", "code")
	q.Set("scope", consoleScope)
	q.Set("state", state)

	return &LoginResult{
		AuthorizeURL: signinBase + authorizePath + "?" + q.Encode(),
		state:        state,
		verifier:     verifier,
	}, nil
}

// Complete exchanges the pasted cross-device code for tokens and STS.
// codeOrURL accepts the raw pasted blob: a base64 "code=...&state=..."
// payload, a plain authorization code, or a full callback URL.
func (l *LoginResult) Complete(ctx context.Context, client *http.Client, codeOrURL string) (*Credentials, error) {
	code, err := extractAuthCode(codeOrURL)
	if err != nil {
		return nil, err
	}
	tok, err := exchangeOAuthCode(ctx, client, code, l.verifier)
	if err != nil {
		return nil, err
	}
	sts, err := parseAccessSTS(tok.AccessToken)
	if err != nil {
		return nil, err
	}
	return &Credentials{
		RefreshToken: tok.RefreshToken,
		ClientID:     crossDeviceID,
		Scope:        tok.Scope,
		AK:           sts.AccessKeyID,
		SK:           sts.SecretAccessKey,
		SessionToken: sts.SessionToken,
		STSSource:    "oauth",
		STSExpiresAt: time.Now().Add(time.Duration(tok.ExpiresIn) * time.Second),
	}, nil
}

func exchangeOAuthCode(ctx context.Context, client *http.Client, code, verifier string) (*oauthTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", signinBase+authorizePath)
	form.Set("client_id", crossDeviceID)
	form.Set("code_verifier", verifier)
	return postTokenForm(ctx, client, form)
}

func refreshOAuthToken(ctx context.Context, client *http.Client, clientID, scope, refreshToken string) (*oauthTokenResponse, error) {
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("client_id", clientID)
	form.Set("refresh_token", refreshToken)
	if scope != "" {
		form.Set("scope", scope)
	}
	return postTokenForm(ctx, client, form)
}

func postTokenForm(ctx context.Context, client *http.Client, form url.Values) (*oauthTokenResponse, error) {
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, signinBase+tokenPath, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("auth: token request failed: %w", err)
	}
	defer resp.Body.Close()
	var raw struct {
		oauthTokenResponse
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 1<<20))
	if err := dec.Decode(&raw); err != nil {
		return nil, fmt.Errorf("auth: decode token response: %w", err)
	}
	if resp.StatusCode/100 != 2 {
		return nil, fmt.Errorf("auth: token endpoint returned %d: %s %s", resp.StatusCode, raw.Error, raw.ErrorDescription)
	}
	if raw.AccessToken == "" {
		return nil, errors.New("auth: token response missing access_token")
	}
	if raw.ExpiresIn <= 0 {
		raw.ExpiresIn = 900
	}
	return &raw.oauthTokenResponse, nil
}

func parseAccessSTS(accessToken string) (*stsTriple, error) {
	trimmed := strings.TrimSpace(accessToken)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		var inner string
		if err := json.Unmarshal([]byte(trimmed), &inner); err != nil {
			return nil, fmt.Errorf("auth: access_token is not a JSON string: %w", err)
		}
		trimmed = inner
	}
	var sts stsTriple
	if err := json.Unmarshal([]byte(trimmed), &sts); err != nil {
		return nil, fmt.Errorf("auth: parse access token STS: %w", err)
	}
	if sts.Data != nil {
		sts = *sts.Data
	}
	if sts.AccessKeyID == "" || sts.SecretAccessKey == "" || sts.SessionToken == "" {
		return nil, errors.New("auth: access token does not contain an STS triple (access_key_id/secret_access_key/session_token)")
	}
	return &sts, nil
}

// extractAuthCode accepts the cross-device callback payload formats.
func extractAuthCode(pasted string) (string, error) {
	pasted = strings.TrimSpace(strings.Trim(pasted, "`"))
	if pasted == "" {
		return "", errors.New("auth: empty authorization code")
	}
	if strings.HasPrefix(pasted, "http://") || strings.HasPrefix(pasted, "https://") {
		u, err := url.Parse(pasted)
		if err == nil {
			if c := u.Query().Get("code"); c != "" {
				return c, nil
			}
		}
	}
	for _, enc := range []*base64.Encoding{
		base64.RawURLEncoding, base64.URLEncoding,
		base64.RawStdEncoding, base64.StdEncoding,
	} {
		if dec, err := enc.DecodeString(pasted); err == nil {
			if q, err := url.ParseQuery(string(dec)); err == nil && q.Get("code") != "" {
				return q.Get("code"), nil
			}
		}
	}
	if q, err := url.ParseQuery(pasted); err == nil && q.Get("code") != "" {
		return q.Get("code"), nil
	}
	return pasted, nil
}

func pkceChallenge(verifier string) string {
	sum := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

func verifierString() (string, error) {
	b := make([]byte, verifierBytes)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func randomHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", b), nil
}

// Load reads the cached credentials file. If nomad has never logged in
// but the public arkcli is installed and authenticated, its stored API
// key is reused so the tool works with zero extra setup.
func Load(path string) (*Credentials, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		var c Credentials
		if err := json.Unmarshal(data, &c); err != nil {
			return nil, fmt.Errorf("auth: parse %s: %w", path, err)
		}
		if c.APIKey != "" {
			return &c, nil
		}
	}
	if key, project := discoverArkCLIKey(); key != "" {
		return &Credentials{APIKey: key, ProjectName: project, STSSource: "arkcli"}, nil
	}
	return nil, err
}

func discoverArkCLIKey() (key, project string) {
	for _, name := range []string{"ARK_API_KEY", "VOLCENGINE_ARK_API_KEY"} {
		if v := strings.TrimSpace(os.Getenv(name)); v != "" {
			return v, strings.TrimSpace(os.Getenv("ARK_PROJECT_NAME"))
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", ""
	}
	dir := filepath.Join(home, ".arkcli")
	if k, p := readEnvKeyProject(filepath.Join(dir, ".env")); k != "" {
		return k, p
	}
	if k, p := readConfigYAMLKeyProject(filepath.Join(dir, "config.yaml")); k != "" {
		return k, p
	}
	k, p := readIdentityKeyProject(filepath.Join(dir, "identities"))
	return k, p
}

func readEnvKeyProject(path string) (key, project string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trim := strings.TrimSpace(line)
		k, v, ok := strings.Cut(trim, "=")
		if !ok {
			continue
		}
		val := strings.Trim(strings.TrimSpace(v), `"'`)
		switch k {
		case "VOLCENGINE_ARK_API_KEY", "ARK_API_KEY":
			key = val
		case "VOLCENGINE_ARK_PROJECT_NAME", "ARK_PROJECT_NAME":
			project = val
		}
	}
	return key, project
}

func readConfigYAMLKeyProject(path string) (key, project string) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		trim := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trim, "api_key:"):
			key = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trim, "api_key:")), `"'`)
		case strings.HasPrefix(trim, "project:"):
			project = strings.Trim(strings.TrimSpace(strings.TrimPrefix(trim, "project:")), `"'`)
		}
	}
	return key, project
}

func readIdentityKeyProject(dir string) (key, project string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", ""
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if data, err := os.ReadFile(filepath.Join(dir, e.Name(), "apikey.json")); err == nil {
			var doc struct {
				APIKey      string `json:"api_key"`
				ProjectName string `json:"project_name"`
			}
			if json.Unmarshal(data, &doc) == nil && strings.TrimSpace(doc.APIKey) != "" {
				project = strings.TrimSpace(doc.ProjectName)
				key = strings.TrimSpace(doc.APIKey)
			}
		}
	}
	return key, project
}

func DiscoverArkCLISTS() (ak, sk, token string, ok bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(filepath.Join(home, ".arkcli", "identities"))
	if err != nil {
		return
	}
	now := time.Now().UnixMilli()
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		data, err := os.ReadFile(filepath.Join(home, ".arkcli", "identities", e.Name(), "sts.json"))
		if err != nil {
			continue
		}
		var doc struct {
			AK        string `json:"ak"`
			SK        string `json:"sk"`
			Token     string `json:"session_token"`
			ExpiresAt int64  `json:"expires_at"`
		}
		if json.Unmarshal(data, &doc) != nil {
			continue
		}
		if doc.AK != "" && doc.ExpiresAt > now {
			return doc.AK, doc.SK, doc.Token, true
		}
	}
	return
}

// Save writes credentials with 0600 permissions.
func Save(path string, c *Credentials) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func (c *Credentials) HasSTS() bool {
	return c.AK != "" && c.SK != "" && c.SessionToken != "" &&
		time.Now().Before(c.STSExpiresAt.Add(-stsSkew))
}

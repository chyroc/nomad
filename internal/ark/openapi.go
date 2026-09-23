package ark

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"

	"github.com/volcengine/volcengine-go-sdk/service/ark"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"
)

// ArkAPIKeyInfo is the result of minting an Ark runtime API key.
type ArkAPIKeyInfo struct {
	APIKey      string
	ID          string
	SID         string
	ProjectName string
}

// openAPIClient builds an Ark OpenAPI client signed with an STS triple.
func openAPIClient(ak, sk, token string) (*ark.ARK, error) {
	return openAPIClientHost(ak, sk, token, DefaultTOPHost)
}

func openAPIClientHost(ak, sk, token, host string) (*ark.ARK, error) {
	cfg := volcengine.NewConfig().
		WithRegion("cn-beijing").
		WithCredentials(credentials.NewStaticCredentials(ak, sk, token)).
		WithMaxRetries(2)
	if host != "" {
		cfg = cfg.WithEndpoint(host)
	}
	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, err
	}
	return ark.New(sess), nil
}

// callAction performs a generic signed POST against an Ark OpenAPI
// action and unmarshals Result into out.
func callAction(ctx context.Context, svc *ark.ARK, action string, in map[string]interface{}, out interface{}) error {
	op := &request.Operation{
		Name:       action,
		HTTPMethod: "POST",
		HTTPPath:   "/",
	}
	return doAction(ctx, svc, op, in, out)
}

func getAction(ctx context.Context, svc *ark.ARK, action string, query map[string]string, out interface{}) error {
	op := &request.Operation{
		Name:       action,
		HTTPMethod: "GET",
		HTTPPath:   "/",
	}
	raw := map[string]interface{}{}
	req := svc.NewRequest(op, nil, &raw)
	q := req.HTTPRequest.URL.Query()
	for k, v := range query {
		q.Set(k, v)
	}
	req.HTTPRequest.URL.RawQuery = q.Encode()
	req.HTTPRequest = req.HTTPRequest.WithContext(ctx)
	if err := req.Send(); err != nil {
		return err
	}
	env := map[string]json.RawMessage{}
	blob, _ := json.Marshal(raw)
	if err := json.Unmarshal(blob, &env); err != nil {
		return err
	}
	if r, ok := env["Result"]; ok && out != nil {
		return json.Unmarshal(r, out)
	}
	return nil
}

func doAction(ctx context.Context, svc *ark.ARK, op *request.Operation, in map[string]interface{}, out interface{}) error {
	rawOut := &map[string]interface{}{}
	req := svc.NewRequest(op, in, rawOut)
	req.HTTPRequest = req.HTTPRequest.WithContext(ctx)
	req.HTTPRequest.Header.Set("Content-Type", "application/json; charset=utf-8")
	if err := req.Send(); err != nil {
		return err
	}
	env := map[string]json.RawMessage{}
	raw, _ := json.Marshal(*rawOut)
	if err := json.Unmarshal(raw, &env); err != nil {
		return err
	}
	if out != nil {
		if r, ok := env["Result"]; ok {
			if err := json.Unmarshal(r, out); err != nil {
				return err
			}
		}
	}
	return nil
}

// CreateArkAPIKey mints a long-lived all-resources Ark runtime API key
// using the user's STS triple. This is the public Ark OpenAPI action
// CreateApiKey (Version 2024-01-01), the same one used by the console.
// project names the IAM project the key belongs to. CreateApiKey returns
// only the key id, so the plaintext key is fetched afterwards with
// GetRawApiKey.
func CreateArkAPIKey(ctx context.Context, ak, sk, token, name, project string) (*ArkAPIKeyInfo, error) {
	svc, err := openAPIClient(ak, sk, token)
	if err != nil {
		return nil, err
	}
	in := map[string]interface{}{
		"Name":        name,
		"ProjectName": project,
		"ResourceInstances": []map[string]string{
			{"ResourceType": "all", "ResourceId": "*"},
		},
		"AccessControlInfo": map[string]bool{"AllowAll": true},
		"IPWhiteListInfo":   map[string]bool{"Enabled": false},
	}
	created := map[string]interface{}{}
	if err := callAction(ctx, svc, "CreateApiKey", in, &created); err != nil {
		return nil, fmt.Errorf("CreateApiKey: %w", err)
	}
	id := firstString(created, "Id", "ID", "ApiKeyId")
	sid := firstString(created, "SID", "ApiKeySID", "api_key_sid")
	if key := firstString(created, "ApiKey", "RawApiKey", "api_key", "Key", "Secret"); key != "" {
		return &ArkAPIKeyInfo{APIKey: key, ID: id, SID: sid}, nil
	}
	if id == "" {
		id = findAPIKeyID(ctx, svc, project, name)
	}
	if id == "" {
		return nil, fmt.Errorf("CreateApiKey response missing key id")
	}
	raw := map[string]interface{}{}
	getIn := map[string]interface{}{"Id": id}
	if n, err := strconv.ParseInt(id, 10, 64); err == nil {
		getIn["Id"] = n
	}
	if project != "" {
		getIn["ProjectName"] = project
	}
	if err := callAction(ctx, svc, "GetRawApiKey", getIn, &raw); err != nil {
		return nil, fmt.Errorf("GetRawApiKey: %w", err)
	}
	key := firstString(raw, "ApiKey", "RawApiKey", "api_key", "Key", "Secret")
	if key == "" {
		return nil, fmt.Errorf("GetRawApiKey response missing api key")
	}
	if sid == "" {
		sid = firstString(raw, "SID", "ApiKeySID", "api_key_sid")
	}
	return &ArkAPIKeyInfo{APIKey: key, ID: id, SID: sid}, nil
}

// findAPIKeyID looks up the id of a freshly created key by listing the
// project's API keys and matching its name.
func findAPIKeyID(ctx context.Context, svc *ark.ARK, project, name string) string {
	in := map[string]interface{}{"ProjectName": project, "PageNumber": 1, "PageSize": 100}
	var out map[string]interface{}
	if err := callAction(ctx, svc, "ListApiKeys", in, &out); err != nil {
		return ""
	}
	var rows []interface{}
	for _, k := range []string{"Items", "ApiKeys", "List", "Data"} {
		if arr, ok := out[k].([]interface{}); ok {
			rows = arr
			break
		}
	}
	for _, row := range rows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		if n, _ := m["Name"].(string); n != name {
			continue
		}
		if id := firstString(m, "Id", "ID", "ApiKeyId"); id != "" {
			return id
		}
	}
	return ""
}

func firstString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			if v != "" {
				return v
			}
		case float64:
			if v != 0 {
				return strconv.FormatInt(int64(v), 10)
			}
		}
	}
	return ""
}

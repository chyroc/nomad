package ark

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/volcengine/volcengine-go-sdk/service/ark"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/request"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"
)

// ArkAPIKeyInfo is the result of minting an Ark runtime API key.
type ArkAPIKeyInfo struct {
	APIKey      string
	ID          int64
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
func CreateArkAPIKey(ctx context.Context, ak, sk, token, name string) (*ArkAPIKeyInfo, error) {
	svc, err := openAPIClient(ak, sk, token)
	if err != nil {
		return nil, err
	}
	var result struct {
		ApiKey string `json:"ApiKey"`
		Id     int64  `json:"Id"`
		SID    string `json:"SID"`
	}
	in := map[string]interface{}{
		"Name": name,
		"ResourceInstances": []map[string]string{
			{"ResourceType": "all", "ResourceId": "*"},
		},
		"AccessControlInfo": map[string]bool{"AllowAll": true},
		"IPWhiteListInfo":   map[string]bool{"Enabled": false},
	}
	if err := callAction(ctx, svc, "CreateApiKey", in, &result); err != nil {
		return nil, fmt.Errorf("CreateApiKey: %w", err)
	}
	if result.ApiKey == "" {
		return nil, fmt.Errorf("CreateApiKey response missing ApiKey")
	}
	return &ArkAPIKeyInfo{APIKey: result.ApiKey, ID: result.Id, SID: result.SID}, nil
}

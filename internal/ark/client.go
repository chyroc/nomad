// Package ma is the only backend: a self-hosted coding agent worker on
// top of the remote Volcengine Ark managed-agents service.
//
// It owns provisioning (self-hosted environment + coding agent), session
// lifecycle, event streaming, local tool execution (via the official
// ark-runtime self-hosted SDK), model listing and file uploads.
package ark

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"time"

	arkruntime "github.com/volcengine/ark-runtime-go/arkruntime"
	"github.com/volcengine/ark-runtime-go/arkruntime/model/file"
)

// Client wraps the runtime client with the few calls nomad needs.
type Client struct {
	rt         *arkruntime.Client
	baseURL    string
	topHost    string
	hc         *http.Client
	apiKey     string
	project    string
	sts        func() (ak, sk, token string, ok bool)
	modelCache string
}

type ClientOption func(*Client)

func WithSTSProvider(f func() (ak, sk, token string, ok bool)) ClientOption {
	return func(c *Client) { c.sts = f }
}

func WithModelCache(path string) ClientOption {
	return func(c *Client) { c.modelCache = path }
}

func NewClient(baseURL, apiKey, project string, opts ...ClientOption) *Client {
	hc := &http.Client{Timeout: 10 * time.Minute}
	if project != "" {
		hc.Transport = projectHeaderTransport{project: project, base: http.DefaultTransport}
	}
	rt := arkruntime.NewClientWithApiKey(apiKey,
		arkruntime.WithBaseUrl(baseURL),
		arkruntime.WithHTTPClient(hc),
	)
	c := &Client{rt: rt, baseURL: baseURL, topHost: DefaultTOPHost, hc: hc, apiKey: apiKey, project: project}
	for _, o := range opts {
		o(c)
	}
	return c
}

type projectHeaderTransport struct {
	project string
	base    http.RoundTripper
}

func (t projectHeaderTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.project != "" {
		req.Header.Set("X-Project-Name", t.project)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(req)
}

func (c *Client) Project() string { return c.project }

// Runtime exposes the underlying SDK client (used by self-host worker).
func (c *Client) Runtime() *arkruntime.Client { return c.rt }

// ModelInfo describes a selectable model.
type ModelInfo struct {
	ID            string   `json:"id"`
	RuntimeID     string   `json:"runtime_id"`
	Name          string   `json:"name"`
	Version       string   `json:"version"`
	ToolCalling   bool     `json:"tool_calling"`
	ContextWindow int      `json:"context_window"`
	ModelVersion  string   `json:"model_version,omitempty"`
	Primary       bool     `json:"primary,omitempty"`
	Efforts       []string `json:"efforts,omitempty"`
}

func (m ModelInfo) SelectID() string {
	if m.RuntimeID != "" {
		return m.RuntimeID
	}
	if m.Name != "" && m.Version != "" {
		return m.Name + "-" + m.Version
	}
	return m.ID
}

func (c *Client) listInferenceModels(ctx context.Context) ([]ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/models", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("ma: list models: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return nil, decodeAPIError("list models", resp)
	}
	var out struct {
		Data []struct {
			ID         string `json:"id"`
			Name       string `json:"name"`
			Status     string `json:"status"`
			Modalities *struct {
				Input  []string `json:"input_modalities"`
				Output []string `json:"output_modalities"`
			} `json:"modalities"`
			TaskType []string `json:"task_type"`
			Features struct {
				Tools *struct {
					FunctionCalling bool `json:"function_calling"`
				} `json:"tools"`
			} `json:"features"`
			TokenLimits struct {
				ContextWindow int `json:"context_window"`
			} `json:"token_limits"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var models []ModelInfo
	for _, m := range out.Data {
		if m.Status == "Shutdown" || m.ID == "" || seen[m.ID] {
			continue
		}
		if !containsStr(m.TaskType, "TextGeneration") {
			continue
		}
		if m.Modalities != nil && !containsStr(m.Modalities.Output, "text") {
			continue
		}
		seen[m.ID] = true
		name := m.Name
		if name == "" {
			name = m.ID
		}
		info := ModelInfo{ID: m.ID, Name: name, ContextWindow: m.TokenLimits.ContextWindow}
		if m.Features.Tools != nil {
			info.ToolCalling = m.Features.Tools.FunctionCalling
		}
		models = append(models, info)
	}
	sort.Slice(models, func(i, j int) bool { return models[i].Name < models[j].Name })
	return models, nil
}

func containsStr(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// UploadFile uploads a local file for use in a multimodal message.
func (c *Client) UploadFile(ctx context.Context, purpose string, r io.Reader) (string, error) {
	req := &file.FileCreateRequest{Purpose: file.Purpose(purpose)}
	obj, err := c.rt.UploadFile(ctx, req, r)
	if err != nil {
		return "", fmt.Errorf("ma: upload file: %w", err)
	}
	return obj.ID, nil
}

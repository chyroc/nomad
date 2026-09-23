package ark

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"sort"
)

func (c *Client) ListModels(ctx context.Context) ([]ModelInfo, error) {
	if models, err := c.listAgentModelsSigned(ctx); err == nil && len(models) > 0 {
		c.writeModelCache(models)
		return models, nil
	}
	if models, ok := c.readModelCache(); ok && len(models) > 0 {
		return models, nil
	}
	return c.listInferenceModels(ctx)
}

func (c *Client) readModelCache() ([]ModelInfo, bool) {
	if c.modelCache == "" {
		return nil, false
	}
	data, err := os.ReadFile(c.modelCache)
	if err != nil {
		return nil, false
	}
	var models []ModelInfo
	if err := json.Unmarshal(data, &models); err != nil || len(models) == 0 {
		return nil, false
	}
	return collapseModelVersions(models), true
}

func (c *Client) writeModelCache(models []ModelInfo) {
	if c.modelCache == "" {
		return
	}
	data, err := json.MarshalIndent(models, "", "  ")
	if err != nil {
		return
	}
	_ = os.WriteFile(c.modelCache, data, 0o600)
}

func (c *Client) listAgentModelsSigned(ctx context.Context) ([]ModelInfo, error) {
	ak, sk, token, ok := "", "", "", false
	if c.sts != nil {
		ak, sk, token, ok = c.sts()
	}
	if !ok {
		return nil, ErrNoSTS
	}
	svc, err := openAPIClientHost(ak, sk, token, c.topHost)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data []struct {
			ID            string `json:"Id"`
			RuntimeID     string `json:"Model"`
			Name          string `json:"Name"`
			Version       string `json:"Version"`
			Status        string `json:"Status"`
			Primary       bool   `json:"PrimaryVersion"`
			RouterSupport bool   `json:"RouterBaselineSupport"`
			ModelMeta     []struct {
				Key  string        `json:"Key"`
				Data []interface{} `json:"Data"`
			} `json:"ModelMetaDatas"`
			Features struct {
				Tools struct {
					FunctionCalling bool `json:"FunctionCalling"`
				} `json:"Tools"`
			} `json:"Features"`
		} `json:"data"`
	}
	query := map[string]string{"Usage": "agent"}
	if c.project != "" {
		query["ProjectName"] = c.project
	}
	if err := getAction(ctx, svc, "ArkModels", query, &resp); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var out []ModelInfo
	for _, m := range resp.Data {
		if m.Status != "Published" || m.ID == "" || seen[m.ID] {
			continue
		}
		agentCapable := false
		var efforts []string
		for _, meta := range m.ModelMeta {
			if meta.Key == "ep_agent.support" {
				for _, d := range meta.Data {
					if s, ok := d.(string); ok && s == "true" {
						agentCapable = true
					}
				}
			}
			if meta.Key == "reasoning_effort.support_value" {
				for _, d := range meta.Data {
					if s, ok := d.(string); ok && s != "" {
						efforts = append(efforts, s)
					}
				}
			}
		}
		if !agentCapable {
			continue
		}
		seen[m.ID] = true
		name := m.Name
		if name == "" {
			name = m.ID
		}
		runtimeID := m.RuntimeID
		if runtimeID == "" && m.Version != "" {
			runtimeID = name + "-" + m.Version
		}
		out = append(out, ModelInfo{
			ID:           m.ID,
			RuntimeID:    runtimeID,
			Name:         name,
			Version:      m.Version,
			ToolCalling:  true,
			ModelVersion: m.Version,
			Primary:      m.Primary,
			Efforts:      efforts,
		})
	}
	return collapseModelVersions(out), nil
}

// collapseModelVersions keeps one entry per model name: the primary
// version when one exists, otherwise the newest version. The result is
// sorted by model name.
func collapseModelVersions(models []ModelInfo) []ModelInfo {
	byName := map[string]ModelInfo{}
	for _, m := range models {
		if existing, ok := byName[m.Name]; !ok || preferModelVersion(m, existing) {
			byName[m.Name] = m
		}
	}
	out := make([]ModelInfo, 0, len(byName))
	for _, m := range byName {
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Name != out[j].Name {
			return out[i].Name < out[j].Name
		}
		return out[i].Version > out[j].Version
	})
	return out
}

func preferModelVersion(candidate, existing ModelInfo) bool {
	if candidate.Primary != existing.Primary {
		return candidate.Primary
	}
	return candidate.Version > existing.Version
}

func preferredModel(models []ModelInfo) string {
	primary, tool := -1, -1
	for i, m := range models {
		if m.Primary {
			if primary == -1 || (m.ToolCalling && !models[primary].ToolCalling) {
				primary = i
			}
		}
		if m.ToolCalling {
			if tool == -1 || m.ContextWindow > models[tool].ContextWindow {
				tool = i
			}
		}
	}
	if tool >= 0 {
		return models[tool].SelectID()
	}
	if primary >= 0 {
		return models[primary].SelectID()
	}
	if len(models) > 0 {
		return models[0].SelectID()
	}
	return ""
}

var ErrNoSTS = errors.New("no valid STS credentials for signed OpenAPI call")

var effortRank = map[string]int{
	"none": 0, "minimal": 1, "low": 2, "medium": 3, "high": 4, "xhigh": 5, "ultra": 5, "max": 6,
}

func ResolveEffort(requested string, supported []string) string {
	want := requested
	if want == "" {
		want = "max"
	}
	for _, e := range supported {
		if e == want {
			return want
		}
	}
	best, bestRank := "", -1
	for _, e := range supported {
		if r, ok := effortRank[e]; ok && r > bestRank {
			best, bestRank = e, r
		}
	}
	if best != "" {
		return best
	}
	return want
}

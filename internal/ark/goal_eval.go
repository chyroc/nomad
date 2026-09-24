package ark

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// GoalVerdict is the tool-free evaluator decision for one turn.
type GoalVerdict struct {
	Met        bool
	Impossible bool
	Reason     string
}

type goalVerdictPayload struct {
	Met        bool   `json:"met"`
	Impossible bool   `json:"impossible"`
	Reason     string `json:"reason"`
	OK         *bool  `json:"ok"`
}

// EvaluateGoal runs one tool-free chat completion that judges whether the
// condition is satisfied from the supplied transcript text. It is a plain
// inference call (no session, no tools), so it cannot mutate the
// workspace.
func (c *Client) EvaluateGoal(ctx context.Context, modelID, systemPrompt, userPrompt string) (GoalVerdict, error) {
	if modelID == "" {
		return GoalVerdict{}, fmt.Errorf("ma: goal evaluator needs a model id")
	}
	body := map[string]interface{}{
		"model": modelID,
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": userPrompt},
		},
		"temperature": 0,
		"max_tokens":  1024,
	}
	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	callCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	if err := c.doJSON(callCtx, "POST", "/chat/completions", body, &out); err != nil {
		return GoalVerdict{}, err
	}
	if len(out.Choices) == 0 {
		return GoalVerdict{}, fmt.Errorf("ma: goal evaluator returned no choices")
	}
	raw := out.Choices[0].Message.Content
	payload, err := parseGoalVerdict(raw)
	if err != nil {
		return GoalVerdict{}, err
	}
	v := GoalVerdict{Met: payload.Met, Impossible: payload.Impossible, Reason: strings.TrimSpace(payload.Reason)}
	if payload.OK != nil {
		v.Met = *payload.OK
	}
	if v.Impossible {
		v.Met = false
	}
	return v, nil
}

func parseGoalVerdict(raw string) (goalVerdictPayload, error) {
	text := strings.TrimSpace(raw)
	text = strings.TrimSpace(strings.TrimPrefix(text, "```json"))
	text = strings.TrimSpace(strings.TrimPrefix(text, "```"))
	text = strings.TrimSuffix(text, "```")
	start, end := strings.Index(text, "{"), strings.LastIndex(text, "}")
	if start < 0 || end <= start {
		return goalVerdictPayload{}, fmt.Errorf("ma: goal evaluator returned non-JSON output: %q", truncateForError(raw))
	}
	var p goalVerdictPayload
	if err := json.Unmarshal([]byte(text[start:end+1]), &p); err != nil {
		return goalVerdictPayload{}, fmt.Errorf("ma: goal evaluator JSON decode: %w", err)
	}
	return p, nil
}

func truncateForError(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 200 {
		return s[:200] + "…"
	}
	return s
}

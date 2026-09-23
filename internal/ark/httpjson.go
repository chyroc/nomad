package ark

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

// doJSON performs a runtime-plane JSON call with the Bearer API key.
func (c *Client) doJSON(ctx context.Context, method, path string, body interface{}, out interface{}) error {
	var rdr io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rdr = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, rdr)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if c.project != "" {
		req.Header.Set("X-Project-Name", c.project)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.hc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("%s %s: %s", method, path, parseAPIErrorMessage(resp.StatusCode, raw))
	}
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return fmt.Errorf("decode %s response: %w", path, err)
		}
	}
	return nil
}

func parseAPIErrorMessage(status int, raw []byte) string {
	var env struct {
		Code    string `json:"code"`
		Message string `json:"message"`
		Error   *struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &env) == nil {
		if env.Error != nil && env.Error.Message != "" {
			return fmt.Sprintf("HTTP %d %s: %s", status, env.Error.Code, env.Error.Message)
		}
		if env.Message != "" {
			return fmt.Sprintf("HTTP %d %s: %s", status, env.Code, env.Message)
		}
	}
	return fmt.Sprintf("HTTP %d: %s", status, string(bytes.TrimSpace(raw)))
}

func decodeAPIError(op string, resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	return fmt.Errorf("%s: %s", op, parseAPIErrorMessage(resp.StatusCode, raw))
}

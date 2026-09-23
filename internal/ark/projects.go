package ark

import (
	"context"

	"github.com/volcengine/volcengine-go-sdk/service/iam20210801"
	"github.com/volcengine/volcengine-go-sdk/volcengine"
	"github.com/volcengine/volcengine-go-sdk/volcengine/credentials"
	"github.com/volcengine/volcengine-go-sdk/volcengine/session"
)

const iamHost = "https://iam.volcengineapi.com"

func iamClient(ak, sk, token string) (*iam20210801.IAM20210801, error) {
	cfg := volcengine.NewConfig().
		WithRegion("cn-north-1").
		WithEndpoint(iamHost).
		WithCredentials(credentials.NewStaticCredentials(ak, sk, token)).
		WithMaxRetries(1)
	sess, err := session.NewSession(cfg)
	if err != nil {
		return nil, err
	}
	return iam20210801.New(sess), nil
}

// ListProjectNames lists the IAM projects visible to the account via the
// signed ListProjects action (Version 2021-08-01). A freshly minted OAuth
// STS has no default project, so login must pick one before calling
// project-scoped actions such as CreateApiKey.
func ListProjectNames(ctx context.Context, ak, sk, token string) ([]string, error) {
	svc, err := iamClient(ak, sk, token)
	if err != nil {
		return nil, err
	}
	var names []string
	seen := map[string]bool{}
	offset, limit := 0, 100
	for {
		in := map[string]interface{}{"Limit": limit, "Offset": offset}
		out, err := svc.ListProjectsCommon(&in)
		if err != nil {
			return nil, err
		}
		batch, total := parseProjectList(*out)
		for _, n := range batch {
			if n != "" && !seen[n] {
				seen[n] = true
				names = append(names, n)
			}
		}
		if len(batch) == 0 || (total > 0 && offset+len(batch) >= total) || offset > 1000 {
			break
		}
		offset += len(batch)
	}
	return names, nil
}

func parseProjectList(env map[string]interface{}) (names []string, total int) {
	result, _ := env["Result"].(map[string]interface{})
	if result == nil {
		result = env
	}
	switch t := result["Total"].(type) {
	case float64:
		total = int(t)
	}
	if total == 0 {
		if t, ok := result["TotalCount"].(float64); ok {
			total = int(t)
		}
	}
	var rows []interface{}
	for _, key := range []string{"ProjectList", "Projects", "Items", "List"} {
		if arr, ok := result[key].([]interface{}); ok && arr != nil {
			rows = arr
			break
		}
	}
	for _, row := range rows {
		m, ok := row.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := m["ProjectName"].(string)
		if name == "" {
			name, _ = m["Name"].(string)
		}
		if status, _ := m["Status"].(string); status != "" && status != "active" && status != "Active" {
			continue
		}
		names = append(names, name)
	}
	return names, total
}

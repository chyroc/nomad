package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Permanent tool-block rendering. A completed tool call settles into
// the transcript as a block:
//
//	● Write(path)
//	  ⎿  Wrote 22 lines to path
//
// followed by a short preview (write content with line numbers, a
// colored edit diff, or the first lines of shell output) with the
// remainder available through the Ctrl+O fold. Browsing actions
// (glob/grep/ls) fold into the surrounding Thought line instead of
// producing a block.

const (
	toolFoldThreshold = 4
	toolFoldShown     = 3
	toolPreviewWidth  = 100
	toolResultIndent  = "  "
)

var browseTools = map[string]bool{
	"glob": true,
	"grep": true,
	"ls":   true,
	"list": true,
}

func isBrowseTool(name string) bool { return browseTools[name] }

type toolBlock struct {
	header string
	lines  []string
	fold   []string
}

// buildToolBlock renders one completed call into its permanent block.
func (a *App) buildToolBlock(name, args, result string, isErr bool, start, end time.Time) toolBlock {
	width, _ := cachedTermSize()
	if width < 20 {
		width = 80
	}
	summary, preview, fold := a.toolBlockParts(name, args, result, isErr, start, end, width)
	block := toolBlock{header: a.toolHeader(name, args, width)}
	block.lines = append(block.lines, a.resultLine(summary, isErr))
	block.lines = append(block.lines, preview...)
	block.fold = fold
	return block
}

// webToolKind classifies a (possibly MCP-namespaced) web tool by
// substring matching, returning "search", "fetch" or "".
func webToolKind(name string) string {
	n := strings.ToLower(name)
	switch {
	case strings.Contains(n, "web_search") || strings.Contains(n, "websearch") || n == "search":
		return "search"
	case strings.Contains(n, "web_fetch") || strings.Contains(n, "webfetch") || strings.Contains(n, "fetch_url") || n == "fetch":
		return "fetch"
	}
	return ""
}

// webArgString pulls the human-visible argument out of a web tool call
// whose schema is not controlled by us. It understands the managed
// agents request-list shape ({search,fetch}_request_list:[{query|url}])
// as well as flat/nested MCP layouts, falling back to any string field.
func webArgString(args string, keys ...string) string {
	m := parseArgs(args)
	if s := strictString(m, keys...); s != "" {
		return s
	}
	if s := requestListString(m, keys); s != "" {
		return s
	}
	for _, env := range []string{"input", "arguments", "params", "args"} {
		if inner, ok := m[env].(map[string]interface{}); ok {
			if s := strictString(inner, keys...); s != "" {
				return s
			}
			if s := requestListString(inner, keys); s != "" {
				return s
			}
			if s := anyStringValue(inner, keys...); s != "" {
				return s
			}
		}
	}
	return anyStringValue(m, keys...)
}

// requestListString extracts the first query/url from a
// {search,fetch}_request_list array of objects.
func requestListString(m map[string]interface{}, keys []string) string {
	for _, listKey := range []string{"search_request_list", "fetch_request_list", "request_list", "requests"} {
		raw, ok := m[listKey]
		if !ok {
			continue
		}
		items, ok := raw.([]interface{})
		if !ok || len(items) == 0 {
			continue
		}
		for _, it := range items {
			if obj, ok := it.(map[string]interface{}); ok {
				if s := strictString(obj, keys...); s != "" {
					return s
				}
			} else if s, ok := it.(string); ok && strings.TrimSpace(s) != "" {
				return s
			}
		}
	}
	return ""
}

// strictString returns a real string value for one of the keys,
// ignoring non-string (nested object) fields.
func strictString(m map[string]interface{}, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// anyStringValue returns the first non-empty string value whose key is
// not in the skip list, used as a last-resort argument extraction.
func anyStringValue(m map[string]interface{}, skip ...string) string {
	skipped := map[string]bool{}
	for _, k := range skip {
		skipped[k] = true
	}
	for k, v := range m {
		if skipped[k] {
			continue
		}
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

// webArgInvocation styles the argument tail of a web tool header;
// an empty argument renders no tail at all.
func webArgInvocation(inv string) string {
	if strings.Trim(inv, "()\"") == "" {
		return ""
	}
	return cDim + inv + cReset
}

func (a *App) toolHeader(name, args string, width int) string {
	display, inv := name, toolInvocation(name, args, min(toolPreviewWidth, max(width-6, 20)))
	switch webToolKind(name) {
	case "search":
		q := truncateToWidth(webArgString(args, "query", "q", "search", "keyword", "question", "input"), max(width-18, 12))
		display, inv = "Web Search", webArgInvocation(`("`+q+`")`)
	case "fetch":
		u := truncateToWidth(webArgString(args, "url", "link", "uri", "target", "input"), max(width-10, 12))
		display, inv = "Fetch", webArgInvocation("("+u+")")
	default:
		switch name {
		case "write":
			display = "Write"
		case "edit":
			display = "Edit"
		}
	}
	return "● " + a.style(cBold, display) + inv
}

func (a *App) resultLine(summary string, isErr bool) string {
	mark := a.style(cDim, "⎿")
	if isErr {
		return toolResultIndent + mark + "  " + cRed + summary + cReset
	}
	return toolResultIndent + mark + "  " + a.style(cDim, summary)
}

func (a *App) toolBlockParts(name, args, result string, isErr bool, start, end time.Time, width int) (summary string, preview, fold []string) {
	argsM := parseArgs(args)
	switch webToolKind(name) {
	case "search":
		return fmt.Sprintf("Did %d search in %s", countSearches(result), toolDuration(start, end)), nil, bodyFold(result)
	case "fetch":
		if isErr {
			return errSummary(result, width)
		}
		summary = fmt.Sprintf("Fetched in %s", toolDuration(start, end))
		preview, fold = textPreview(result, width)
		return
	}
	switch {
	case name == "write" || name == "edit":
		path := firstStr(argsM, "file_path", "path")
		added, removed := diffCounts(name, args, a.paths.Workspace)
		dl, _ := toolCallDiff(name, args, a.paths.Workspace)
		preview, fold = diffPreview(dl, width, name == "write")
		return writeSummary(name, path, added, removed), preview, fold
	case name == "bash":
		body, code := stripExitCode(result)
		if isErr {
			return errSummary(body, width)
		}
		first, rest := splitFirstLine(body)
		if first == "" {
			if code != "" && code != "0" {
				return "exit_code: " + code, nil, nil
			}
			return "Done", nil, nil
		}
		preview, fold = bodyPreview(strings.Split(strings.TrimRight(rest, "\n"), "\n"), 1, width)
		return truncateToWidth(first, max(width-6, 20)), preview, fold
	default:
		if isErr {
			return errSummary(result, width)
		}
		first, rest := splitFirstLine(result)
		if first == "" {
			return "(No output)", nil, nil
		}
		preview, fold = bodyPreview(strings.Split(strings.TrimRight(rest, "\n"), "\n"), 1, width)
		return truncateToWidth(first, max(width-6, 20)), preview, fold
	}
}

// errSummary renders the error summary line plus a preview that skips
// the line already shown in the summary.
func errSummary(result string, width int) (summary string, preview, fold []string) {
	first, rest := splitFirstLine(result)
	if first == "" {
		first = "(No output)"
	}
	preview, fold = bodyPreview(strings.Split(strings.TrimRight(rest, "\n"), "\n"), 1, width)
	return "Error: " + truncateToWidth(first, max(width-7, 20)), preview, fold
}

// stripExitCode removes the leading exit-code metadata line the bash
// runner prefixes to results, returning the body and the raw code.
func stripExitCode(result string) (body, code string) {
	s := strings.TrimLeft(result, " \t")
	if !strings.HasPrefix(s, "exit_code:") {
		return result, ""
	}
	rest := s[len("exit_code:"):]
	nl := strings.IndexByte(rest, '\n')
	if nl < 0 {
		return "", strings.TrimSpace(rest)
	}
	return rest[nl+1:], strings.TrimSpace(rest[:nl])
}

// splitFirstLine returns the first non-empty trimmed line and the body
// with that line removed, so the summary never repeats as the first
// preview row.
func splitFirstLine(result string) (first, rest string) {
	s := result
	for {
		nl := strings.IndexByte(s, '\n')
		line := s
		if nl >= 0 {
			line = s[:nl]
		}
		line = strings.TrimSpace(line)
		if line != "" {
			if nl < 0 {
				return line, ""
			}
			return line, s[nl+1:]
		}
		if nl < 0 {
			return "", s
		}
		s = s[nl+1:]
	}
}

func toolDuration(start, end time.Time) string {
	if start.IsZero() || end.IsZero() || !end.After(start) {
		return "0s"
	}
	return formatThoughtDuration(end.Sub(start))
}

// parseArgs decodes a tool call's arguments JSON. The managed-agents
// event input may be a JSON object, or a JSON-encoded string wrapping
// an object, so unwrap one string layer when needed.
func parseArgs(args string) map[string]interface{} {
	raw := []byte(strings.TrimSpace(args))
	m := map[string]interface{}{}
	if err := json.Unmarshal(raw, &m); err == nil {
		return m
	}
	var wrapped string
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		inner := map[string]interface{}{}
		if json.Unmarshal([]byte(wrapped), &inner) == nil {
			return inner
		}
	}
	return m
}

func bodyFold(result string) []string {
	body := strings.TrimSpace(result)
	if body == "" {
		return nil
	}
	return strings.Split(body, "\n")
}

// textPreview shows a textual result indented under the ⎿ row.
func textPreview(result string, width int) (preview, fold []string) {
	return bodyPreview(strings.Split(strings.TrimRight(result, "\n"), "\n"), 0, width)
}

// bodyPreview renders the rows under the ⎿ line the way the reference
// agent does: blocks of up to toolFoldThreshold rows show in full,
// longer blocks show the first toolFoldShown rows and fold the rest
// behind a count marker. leadRows counts rows the summary already
// consumed, so the threshold covers the whole block.
func bodyPreview(lines []string, leadRows, width int) (preview, fold []string) {
	total := leadRows + len(lines)
	if total <= toolFoldThreshold {
		for _, l := range lines {
			preview = append(preview, previewRow(l, width))
		}
		return preview, nil
	}
	fold = lines
	n := min(toolFoldShown-leadRows, len(lines))
	for _, l := range lines[:max(n, 0)] {
		preview = append(preview, previewRow(l, width))
	}
	preview = append(preview, cDim+fmt.Sprintf("     … +%d lines (ctrl+o to expand)", total-toolFoldShown)+cReset)
	return
}

func previewRow(l string, width int) string {
	return "     " + truncateToWidth(l, max(width-6, 20))
}

// diffPreview shows the head of a write/edit change: numbered added
// lines for a fresh write, colored +/- lines for an edit.
func diffPreview(lines []diffLine, width int, numberedWrite bool) (preview, fold []string) {
	if len(lines) == 0 {
		return nil, nil
	}
	for _, l := range lines {
		fold = append(fold, formatDiffLine(l, width))
	}
	n := 0
	for _, l := range lines {
		if len(lines) <= toolFoldThreshold {
			break
		}
		if n >= toolFoldShown {
			break
		}
		if numberedWrite {
			if l.Kind == '+' {
				preview = append(preview, cDim+fmt.Sprintf("%5d ", l.NewNo)+cReset+
					truncateToWidth(strings.TrimPrefix(l.Text, "+"), max(width-6, 20)))
				n++
			}
			continue
		}
		preview = append(preview, formatDiffLine(l, width))
		n++
	}
	if len(lines) > toolFoldThreshold {
		preview = append(preview, cDim+fmt.Sprintf("     … +%d lines (ctrl+o to expand)", len(lines)-n)+cReset)
	} else {
		for _, l := range lines[n:] {
			if numberedWrite && l.Kind != '+' {
				continue
			}
			if numberedWrite {
				preview = append(preview, cDim+fmt.Sprintf("%5d ", l.NewNo)+cReset+
					truncateToWidth(strings.TrimPrefix(l.Text, "+"), max(width-6, 20)))
				continue
			}
			preview = append(preview, formatDiffLine(l, width))
		}
	}
	return
}

func formatDiffLine(l diffLine, width int) string {
	switch l.Kind {
	case '+':
		return cGreen + "+" + truncateToWidth(l.Text, width-1) + cReset
	case '-':
		return cRed + "-" + truncateToWidth(l.Text, width-1) + cReset
	default:
		return cDim + " " + truncateToWidth(l.Text, width-1) + cReset
	}
}

func writeSummary(name, path string, added, removed int) string {
	if name == "edit" {
		var parts []string
		if added > 0 {
			parts = append(parts, fmt.Sprintf("Added %d lines", added))
		}
		if removed > 0 {
			parts = append(parts, fmt.Sprintf("removed %d lines", removed))
		}
		if len(parts) == 0 {
			return "No changes"
		}
		parts[0] = capitalize(parts[0])
		return strings.Join(parts, ", ")
	}
	if added <= 0 {
		return "Wrote " + path
	}
	return fmt.Sprintf("Wrote %d lines to %s", added, path)
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

func diffCounts(name, args, workspace string) (added, removed int) {
	lines, ok := toolCallDiff(name, args, workspace)
	if !ok {
		return 0, 0
	}
	for _, l := range lines {
		switch l.Kind {
		case '+':
			added++
		case '-':
			removed++
		}
	}
	return
}

func countSearches(result string) int {
	var parsed struct {
		Results []json.RawMessage `json:"results"`
		N       int               `json:"num_results"`
	}
	if strings.TrimSpace(result) != "" && json.Unmarshal([]byte(result), &parsed) == nil {
		switch {
		case parsed.N > 0:
			return parsed.N
		case len(parsed.Results) > 0:
			return len(parsed.Results)
		}
	}
	return 1
}

// runningLabel renders the spinner label for an in-flight call.
// Browsing actions use a gerund phrase; others keep "Name(args)".
func runningLabel(name, args string) string {
	argsM := parseArgs(args)
	switch name {
	case "ls", "list":
		return "Listing directory…"
	case "glob":
		dir := firstStr(argsM, "path", "directory")
		if dir != "" {
			return "Listing " + dir + "…"
		}
		return "Listing files…"
	case "grep":
		return "Searching files…"
	case "web_search", "WebSearch":
		return "Searching the web…"
	case "web_fetch", "WebFetch":
		return "Fetching page…"
	}
	return name + " " + toolInvocation(name, args, 40)
}

// for a discovery action, e.g. "listed 1 directory".
func browseSummary(name, result string) string {
	switch name {
	case "ls", "list":
		dirs, _ := countListedEntries(result)
		noun := "directory"
		if dirs != 1 {
			noun = "directories"
		}
		return fmt.Sprintf("listed %d %s", dirs, noun)
	case "glob":
		files := countNonEmptyLines(result)
		noun := "file"
		if files != 1 {
			noun = "files"
		}
		return fmt.Sprintf("found %d %s", files, noun)
	case "grep":
		n := countNonEmptyLines(result)
		noun := "match"
		if n != 1 {
			noun = "matches"
		}
		return fmt.Sprintf("found %d %s", n, noun)
	}
	return ""
}

func countNonEmptyLines(result string) int {
	n := 0
	for _, l := range strings.Split(result, "\n") {
		if strings.TrimSpace(l) != "" {
			n++
		}
	}
	return n
}

func countListedEntries(result string) (dirs, files int) {
	for _, l := range strings.Split(result, "\n") {
		l = strings.TrimSpace(l)
		if l == "" {
			continue
		}
		if strings.HasSuffix(l, "/") {
			dirs++
		} else {
			files++
		}
	}
	if dirs == 0 && files > 0 {
		dirs, files = files, 0
	}
	return
}

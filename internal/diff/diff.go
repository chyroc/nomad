// Package diff computes unified-style line diffs for tool-call
// arguments, shared by the terminal folds and the session web view.
package diff

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const oldFileCap = 256 * 1024

// Line is one rendered diff line. Kind is '@' (hunk header), ' ',
// '+' or '-'. OldNo/NewNo are 1-based line numbers, zero on the side
// the line does not occupy (and zero for hunk headers).
type Line struct {
	Kind  byte
	Text  string
	OldNo int
	NewNo int
}

type op struct {
	op byte
	ai int
	bi int
}

// EditArgs mirrors the arguments of an edit tool call.
type EditArgs struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

// WriteArgs mirrors the arguments of a write tool call.
type WriteArgs struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// ToolCall builds a unified line diff for edit/write tool arguments.
// It returns ok=false for other tools or incomplete args.
func ToolCall(name, argsJSON, workspace string) ([]Line, bool) {
	var filePath, oldText, newText string
	switch name {
	case "edit":
		var args EditArgs
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.FilePath == "" {
			return nil, false
		}
		filePath, oldText, newText = args.FilePath, args.OldString, args.NewString
	case "write":
		var args WriteArgs
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.FilePath == "" {
			return nil, false
		}
		filePath, newText = args.FilePath, args.Content
		oldText = existingFileContent(filePath, workspace)
	default:
		return nil, false
	}
	if oldText == newText {
		return nil, false
	}
	return Unified(oldText, newText), true
}

func existingFileContent(path, workspace string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > oldFileCap {
		return ""
	}
	return string(data)
}

// Unified produces unified-style hunks with one line of context.
func Unified(old, new string) []Line {
	a := splitLines(old)
	b := splitLines(new)
	ops := lineOps(a, b)
	return hunksFromOps(a, b, ops)
}

func splitLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// lineOps returns per-line operations: ' ', '+', '-' via LCS.
func lineOps(a, b []string) []op {
	m, n := len(a), len(b)
	lcs := make([][]int, m+1)
	for i := range lcs {
		lcs[i] = make([]int, n+1)
	}
	for i := m - 1; i >= 0; i-- {
		for j := n - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else if lcs[i+1][j] >= lcs[i][j+1] {
				lcs[i][j] = lcs[i+1][j]
			} else {
				lcs[i][j] = lcs[i][j+1]
			}
		}
	}
	var ops []op
	i, j := 0, 0
	for i < m && j < n {
		switch {
		case a[i] == b[j]:
			ops = append(ops, op{' ', i, j})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, op{'-', i, -1})
			i++
		default:
			ops = append(ops, op{'+', -1, j})
			j++
		}
	}
	for ; i < m; i++ {
		ops = append(ops, op{'-', i, -1})
	}
	for ; j < n; j++ {
		ops = append(ops, op{'+', -1, j})
	}
	return ops
}

func hunksFromOps(a, b []string, ops []op) []Line {
	const context = 1
	var out []Line
	i := 0
	for i < len(ops) {
		if ops[i].op == ' ' {
			i++
			continue
		}
		start := i - context
		if start < 0 {
			start = 0
		}
		end := i
		for end < len(ops) && ops[end].op != ' ' {
			end++
		}
		for end < len(ops) {
			gap, k := 0, end
			for k < len(ops) && ops[k].op == ' ' {
				gap++
				k++
			}
			if k < len(ops) && ops[k].op != ' ' && gap <= context*2 {
				end = k
				for end < len(ops) && ops[end].op != ' ' {
					end++
				}
				continue
			}
			break
		}
		stop := end + context
		if stop > len(ops) {
			stop = len(ops)
		}
		slice := ops[start:stop]
		oldCount, newCount := 0, 0
		for _, o := range slice {
			if o.op != '+' {
				oldCount++
			}
			if o.op != '-' {
				newCount++
			}
		}
		out = append(out, Line{Kind: '@', Text: hunkHeader(
			hunkStart(slice, '-', ops, start), oldCount,
			hunkStart(slice, '+', ops, start), newCount,
		)})
		oldNo := hunkStart(slice, '-', ops, start)
		newNo := hunkStart(slice, '+', ops, start)
		for _, o := range slice {
			switch o.op {
			case ' ':
				out = append(out, Line{Kind: ' ', Text: a[o.ai], OldNo: oldNo, NewNo: newNo})
				oldNo++
				newNo++
			case '-':
				out = append(out, Line{Kind: '-', Text: a[o.ai], OldNo: oldNo})
				oldNo++
			case '+':
				out = append(out, Line{Kind: '+', Text: b[o.bi], NewNo: newNo})
				newNo++
			}
		}
		i = stop
	}
	return out
}

// hunkStart returns the 1-based start line for one side of a hunk: the
// first consumed line on that side, or the position after the preceding
// unchanged line for a pure insertion or deletion.
func hunkStart(slice []op, changed byte, all []op, start int) int {
	for _, o := range slice {
		if o.op == ' ' || o.op == changed {
			if changed == '-' {
				return o.ai + 1
			}
			return o.bi + 1
		}
	}
	if start == 0 {
		return 0
	}
	prev := all[start-1]
	if changed == '-' {
		return prev.ai + 1
	}
	return prev.bi + 1
}

func hunkHeader(oldStart, oldCount, newStart, newCount int) string {
	return "@@ -" + hunkRange(oldStart, oldCount) + " +" + hunkRange(newStart, newCount) + " @@"
}

func hunkRange(start, count int) string {
	if count == 0 {
		return strconv.Itoa(start) + ",0"
	}
	if count == 1 {
		return strconv.Itoa(start)
	}
	return strconv.Itoa(start) + "," + strconv.Itoa(count)
}

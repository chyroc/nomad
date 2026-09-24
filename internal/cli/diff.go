package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

const (
	diffPreviewChanged = 60
	diffOldFileCap     = 256 * 1024
)

type diffLine struct {
	kind byte
	text string
}

type diffOp struct {
	op byte
	ai int
	bi int
}

type editToolArgs struct {
	FilePath  string `json:"file_path"`
	OldString string `json:"old_string"`
	NewString string `json:"new_string"`
}

type writeToolArgs struct {
	FilePath string `json:"file_path"`
	Content  string `json:"content"`
}

// toolCallDiff builds a unified line diff for edit/write tool
// arguments. It returns ok=false for other tools or incomplete args.
func toolCallDiff(name, argsJSON, workspace string) ([]diffLine, bool) {
	var filePath, oldText, newText string
	switch name {
	case "edit":
		var args editToolArgs
		if err := json.Unmarshal([]byte(argsJSON), &args); err != nil || args.FilePath == "" {
			return nil, false
		}
		filePath, oldText, newText = args.FilePath, args.OldString, args.NewString
	case "write":
		var args writeToolArgs
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
	return unifiedDiff(oldText, newText), true
}

func existingFileContent(path, workspace string) string {
	if !filepath.IsAbs(path) {
		path = filepath.Join(workspace, path)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) > diffOldFileCap {
		return ""
	}
	return string(data)
}

// unifiedDiff produces unified-style hunks with one line of context.
func unifiedDiff(old, new string) []diffLine {
	a := splitDiffLines(old)
	b := splitDiffLines(new)
	ops := diffOps(a, b)
	return hunksFromOps(a, b, ops)
}

func splitDiffLines(s string) []string {
	s = strings.TrimSuffix(s, "\n")
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// diffOps returns per-line operations: ' ', '+', '-' via LCS.
func diffOps(a, b []string) []diffOp {
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
	var ops []diffOp
	i, j := 0, 0
	for i < m && j < n {
		switch {
		case a[i] == b[j]:
			ops = append(ops, diffOp{' ', i, j})
			i++
			j++
		case lcs[i+1][j] >= lcs[i][j+1]:
			ops = append(ops, diffOp{'-', i, -1})
			i++
		default:
			ops = append(ops, diffOp{'+', -1, j})
			j++
		}
	}
	for ; i < m; i++ {
		ops = append(ops, diffOp{'-', i, -1})
	}
	for ; j < n; j++ {
		ops = append(ops, diffOp{'+', -1, j})
	}
	return ops
}

func hunksFromOps(a, b []string, ops []diffOp) []diffLine {
	const context = 1
	var out []diffLine
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
		out = append(out, diffLine{kind: '@', text: hunkHeader(
			hunkStart(slice, '-', ops, start), oldCount,
			hunkStart(slice, '+', ops, start), newCount,
		)})
		for _, o := range slice {
			switch o.op {
			case ' ':
				out = append(out, diffLine{' ', a[o.ai]})
			case '-':
				out = append(out, diffLine{'-', a[o.ai]})
			case '+':
				out = append(out, diffLine{'+', b[o.bi]})
			}
		}
		i = stop
	}
	return out
}

// hunkStart returns the 1-based start line for one side of a hunk: the
// first consumed line on that side, or the position after the preceding
// unchanged line for a pure insertion or deletion.
func hunkStart(slice []diffOp, changed byte, all []diffOp, start int) int {
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
		return itoa(start) + ",0"
	}
	if count == 1 {
		return itoa(start)
	}
	return itoa(start) + "," + itoa(count)
}

// changedDiffLines counts added and removed lines.
func changedDiffLines(lines []diffLine) int {
	n := 0
	for _, l := range lines {
		if l.kind == '+' || l.kind == '-' {
			n++
		}
	}
	return n
}

// renderToolDiff prints a colored diff preview below an edit/write
// invocation line, collapsing large diffs into an expandable fold.
func (a *App) renderToolDiff(name, args string) {
	lines, ok := toolCallDiff(name, args, a.paths.Workspace)
	if !ok {
		return
	}
	rendered := make([]string, len(lines))
	for i, l := range lines {
		switch l.kind {
		case '+':
			rendered[i] = cGreen + l.text + cReset
		case '-':
			rendered[i] = cRed + l.text + cReset
		default:
			rendered[i] = cDim + l.text + cReset
		}
	}
	changed := changedDiffLines(lines)
	if changed <= diffPreviewChanged {
		for _, l := range rendered {
			a.printf("  %s\n", l)
		}
		return
	}
	head := 0
	seen := 0
	for head < len(rendered) && seen < diffPreviewChanged {
		if lines[head].kind == '+' || lines[head].kind == '-' {
			seen++
		}
		head++
	}
	const tailCount = 3
	tail := tailCount
	if head+tail > len(rendered) {
		tail = len(rendered) - head
	}
	for _, l := range rendered[:head] {
		a.printf("  %s\n", l)
	}
	a.registerFold(name+" diff", rendered)
	a.printf("%s\n", foldBar(len(rendered)-head-tail))
	for _, l := range rendered[len(rendered)-tail:] {
		a.printf("  %s\n", l)
	}
}

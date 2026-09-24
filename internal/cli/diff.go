package cli

import (
	"github.com/chyroc/nomad/internal/diff"
)

type diffLine = diff.Line

// toolCallDiff builds a unified line diff for edit/write tool
// arguments. It returns ok=false for other tools or incomplete args.
func toolCallDiff(name, argsJSON, workspace string) ([]diffLine, bool) {
	return diff.ToolCall(name, argsJSON, workspace)
}

// unifiedDiff produces unified-style hunks with one line of context.
func unifiedDiff(old, new string) []diffLine {
	return diff.Unified(old, new)
}

// toolCallDiffLines renders the colored unified-diff lines for an
// edit/write call so they can be included in the tool's Ctrl+O fold.
func toolCallDiffLines(name, argsJSON, workspace string) ([]string, bool) {
	lines, ok := diff.ToolCall(name, argsJSON, workspace)
	if !ok {
		return nil, false
	}
	rendered := make([]string, len(lines))
	for i, l := range lines {
		switch l.Kind {
		case '+':
			rendered[i] = cGreen + l.Text + cReset
		case '-':
			rendered[i] = cRed + l.Text + cReset
		default:
			rendered[i] = cDim + l.Text + cReset
		}
	}
	return rendered, true
}

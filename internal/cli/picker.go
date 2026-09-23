package cli

import (
	"bufio"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"golang.org/x/term"
)

type pickItem struct {
	id    string
	label string
	tag   string
	desc  string
}

type picker struct {
	in       *bufio.Reader
	out      io.Writer
	fd       int
	items    []pickItem
	initial  int
	title    string
	subtitle string
	efforts  []string
	effort0  int
	session  bool
	multi    bool
	checked  map[string]bool
}

func newPicker(in io.Reader, out io.Writer, items []pickItem, initial int) *picker {
	return newPickerFull(in, out, items, initial, "", "", nil, 0)
}

func newPickerFull(in io.Reader, out io.Writer, items []pickItem, initial int,
	title, subtitle string, efforts []string, effortIdx int) *picker {
	br, _ := in.(*bufio.Reader)
	if br == nil {
		br = bufio.NewReader(in)
	}
	fd := -1
	if f, ok := out.(interface{ Fd() uintptr }); ok {
		fd = int(f.Fd())
	}
	if initial < 0 || initial >= len(items) {
		initial = 0
	}
	return &picker{in: br, out: out, fd: fd, items: items, initial: initial,
		title: title, subtitle: subtitle, efforts: efforts, effort0: effortIdx}
}

// withSessionSave enables the "s to use this session only" action.
func (p *picker) withSessionSave() *picker {
	p.session = true
	return p
}

// withMultiSelect turns the picker into a checkbox list: Space toggles
// the highlighted row, a toggles all visible rows, Enter confirms the
// checked set.
func (p *picker) withMultiSelect(initial []string) *picker {
	p.multi = true
	p.checked = map[string]bool{}
	for _, id := range initial {
		p.checked[id] = true
	}
	return p
}

func (p *picker) cursorRow() int {
	io.WriteString(p.out, "\x1b[6n")
	var buf []byte
	gotEsc := false
	deadline := time.After(time.Second)
	for {
		b, err := p.in.ReadByte()
		if err != nil {
			return 0
		}
		switch {
		case b == 0x1b:
			gotEsc = true
			buf = buf[:0]
		case gotEsc:
			buf = append(buf, b)
			if b == 'R' {
				parts := strings.Split(strings.TrimSuffix(strings.TrimPrefix(string(buf), "["), "R"), ";")
				if len(parts) == 2 {
					if n, err := strconv.Atoi(parts[0]); err == nil {
						return n
					}
				}
				gotEsc = false
				buf = buf[:0]
			}
		}
		select {
		case <-deadline:
			return 0
		default:
		}
	}
}

type pickResult struct {
	id        string
	ids       []string
	confirmed bool
	session   bool
	effortIdx int
}

func (p *picker) Run() (string, bool) {
	r, ok := p.RunFull()
	return r.id, ok && r.confirmed
}

// RunMulti returns the checked item ids after a multi-select run.
func (p *picker) RunMulti() ([]string, bool) {
	r, ok := p.RunFull()
	return r.ids, ok && r.confirmed
}

func (p *picker) RunFull() (pickResult, bool) {
	if p.fd < 0 {
		return pickResult{}, false
	}
	old, err := term.MakeRaw(p.fd)
	if err != nil {
		return pickResult{}, false
	}
	defer term.Restore(p.fd, old)

	width, height, _ := term.GetSize(p.fd)
	if width <= 0 {
		width = 80
	}
	if height <= 0 {
		height = 24
	}
	cursor := p.cursorRow()
	if cursor <= 0 || cursor > height {
		cursor = height
	}

	const headerRows = 4 // divider, title, subtitle, blank
	footRows := 1
	if len(p.efforts) > 0 {
		footRows++
	}

	var query []rune
	sel := p.initial
	effort := p.effort0
	top := 0
	panelTop := cursor

	filtered := func(q string) []pickItem {
		return filterPickItems(p.items, q)
	}

	draw := func() {
		vis := filtered(string(query))
		if sel >= len(vis) {
			sel = 0
			top = 0
		}
		maxItems := height - headerRows - footRows
		if maxItems < 1 {
			maxItems = 1
		}
		total := len(vis)
		shown := total
		if shown > maxItems {
			shown = maxItems
		}
		listRows := shown
		if listRows == 0 {
			listRows = 1
		}
		needed := headerRows + listRows + footRows
		anchor := cursor
		if anchor+needed-1 > height {
			anchor = height - needed + 1
		}
		if anchor < 1 {
			anchor = 1
		}
		panelTop = anchor
		top = scrollWindow(total, shown, sel, top)

		var sb strings.Builder
		sb.WriteString("\x1b[?25l")
		sb.WriteString(fmt.Sprintf("\x1b[%d;1H\x1b[J", anchor))
		row := anchor
		if p.title != "" {
			sb.WriteString(fmt.Sprintf("\x1b[%d;1H\x1b[2m────────────────────────────────────────────────────────────────\x1b[0m", row))
			row++
			sb.WriteString(fmt.Sprintf("\x1b[%d;1H\x1b[1m  %s\x1b[0m", row, p.title))
			row++
			if p.subtitle != "" {
				sb.WriteString(fmt.Sprintf("\x1b[%d;1H\x1b[2m  %s\x1b[0m", row, truncateToWidth(p.subtitle, width-4)))
				row++
			}
			row++
		}
		if shown == 0 {
			sb.WriteString(fmt.Sprintf("\x1b[%d;1H\x1b[2m    (no matches)%s\x1b[0m", row, strings.Repeat(" ", width)))
			row++
		}
		for i := 0; i < shown; i++ {
			idx := top + i
			if idx >= len(vis) {
				break
			}
			it := vis[idx]
			marker := "    "
			label := it.label
			if p.multi {
				box := "○"
				if p.checked[it.id] {
					box = "◉"
				}
				if idx == sel {
					marker = "  \x1b[36m❯\x1b[0m \x1b[36m" + box + "\x1b[0m "
					label = "\x1b[36m" + it.label + "\x1b[0m"
				} else if p.checked[it.id] {
					marker = "    \x1b[32m" + box + "\x1b[0m "
				} else {
					marker = "    " + box + " "
				}
			} else if idx == sel {
				marker = "  \x1b[36m❯\x1b[0m "
				label = "\x1b[36m" + it.label + "\x1b[0m"
			}
			line := label
			if it.tag != "" {
				line += "  " + it.tag
			}
			if p.multi && it.desc != "" {
				line += "  " + it.desc
			}
			sb.WriteString(fmt.Sprintf("\x1b[%d;1H%s%s", row, marker, truncateToWidth(line, width-6)))
			row++
		}
		if len(p.efforts) > 0 && effort < len(p.efforts) {
			sb.WriteString(fmt.Sprintf("\x1b[%d;1H\x1b[2m  ◉ %s effort  ←/→ to adjust\x1b[0m", row, p.efforts[effort]))
			row++
		}
		foot := "  Enter to confirm · Esc to cancel"
		switch {
		case p.multi:
			n := 0
			for _, it := range vis {
				if p.checked[it.id] {
					n++
				}
			}
			foot = fmt.Sprintf("  Space to toggle · * to toggle all · Enter to confirm %d selected · Esc to cancel", n)
		case p.session:
			foot = "  Enter to set as default · s to use this session only · Esc to cancel"
		}
		sb.WriteString(fmt.Sprintf("\x1b[%d;1H\x1b[2m%s\x1b[0m", row, foot))
		io.WriteString(p.out, sb.String())
	}

	erase := func() {
		io.WriteString(p.out, fmt.Sprintf("\x1b[?25h\x1b[%d;1H\x1b[J", panelTop))
	}

	draw()
	for {
		r, _, err := p.in.ReadRune()
		if err != nil {
			erase()
			return pickResult{}, false
		}
		vis := filtered(string(query))
		switch {
		case r == '\r' || r == '\n':
			if p.multi {
				var ids []string
				for _, it := range p.items {
					if p.checked[it.id] {
						ids = append(ids, it.id)
					}
				}
				erase()
				return pickResult{ids: ids, confirmed: true}, true
			}
			if sel < len(vis) {
				erase()
				return pickResult{id: vis[sel].id, confirmed: true, effortIdx: effort}, true
			}
		case r == ' ' && p.multi:
			if sel < len(vis) {
				id := vis[sel].id
				p.checked[id] = !p.checked[id]
			}
		case r == '*' && p.multi:
			allChecked := len(vis) > 0
			for _, it := range vis {
				if !p.checked[it.id] {
					allChecked = false
					break
				}
			}
			for _, it := range vis {
				p.checked[it.id] = !allChecked
			}
		case r == 's' && p.session && !p.multi:
			if sel < len(vis) {
				erase()
				return pickResult{id: vis[sel].id, confirmed: true, session: true, effortIdx: effort}, true
			}
		case r == 27:
			if p.in.Buffered() == 0 {
				erase()
				return pickResult{}, false
			}
			r2, _, _ := p.in.ReadRune()
			if r2 != '[' {
				erase()
				return pickResult{}, false
			}
			r3, _, _ := p.in.ReadRune()
			switch r3 {
			case 'A':
				if sel > 0 {
					sel--
				}
			case 'B':
				if sel < len(vis)-1 {
					sel++
				}
			case 'C':
				if len(p.efforts) > 0 && effort < len(p.efforts)-1 {
					effort++
				}
			case 'D':
				if len(p.efforts) > 0 && effort > 0 {
					effort--
				}
			}
		case r == 127 || r == 8:
			if len(query) > 0 {
				query = query[:len(query)-1]
			}
		case r >= 32:
			query = append(query, r)
		}
		draw()
	}
}

func scrollWindow(total, shown, sel, top int) int {
	if shown >= total {
		return 0
	}
	if top > sel {
		top = sel
	}
	if sel >= top+shown {
		top = sel - shown + 1
	}
	if top+shown > total {
		top = total - shown
	}
	if top < 0 {
		top = 0
	}
	return top
}

func filterPickItems(items []pickItem, q string) []pickItem {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return items
	}
	var out []pickItem
	for _, it := range items {
		if strings.Contains(strings.ToLower(it.label), q) ||
			strings.Contains(strings.ToLower(it.id), q) ||
			strings.Contains(strings.ToLower(it.tag), q) ||
			strings.Contains(strings.ToLower(it.desc), q) {
			out = append(out, it)
		}
	}
	return out
}

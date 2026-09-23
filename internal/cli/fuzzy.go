package cli

import (
	"sort"
	"strings"
)

// fuzzyScore matches pattern against target as an ordered subsequence
// (case-insensitive). It follows the sahilm/fuzzy scoring idea: first
// characters, separator boundaries and consecutive runs score higher.
// Returns score and whether all pattern characters matched.
func fuzzyScore(pattern, target string) (int, bool) {
	pattern = strings.ToLower(pattern)
	target = strings.ToLower(target)
	if pattern == "" {
		return 0, true
	}
	pi, score, streak := 0, 0, 0
	firstMatch, lastMatch := -1, 0
	for ti := 0; ti < len(target) && pi < len(pattern); ti++ {
		if target[ti] != pattern[pi] {
			streak = 0
			continue
		}
		if firstMatch < 0 {
			firstMatch = ti
		}
		switch {
		case ti == 0:
			score += 8
		case isFuzzySep(target[ti-1]):
			score += 6
		case streak > 0:
			score += 4 + streak
		default:
			score += 1
		}
		lastMatch = ti
		streak++
		pi++
	}
	if pi < len(pattern) {
		return 0, false
	}
	score -= firstMatch
	score -= (lastMatch - firstMatch)
	return score, true
}

func isFuzzySep(b byte) bool {
	return b == ' ' || b == '-' || b == '_' || b == '.' || b == '/' || b == '\\'
}

func filterPickItems(items []pickItem, q string) []pickItem {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return items
	}
	tokens := strings.Fields(q)
	type scored struct {
		it    pickItem
		score int
		idx   int
	}
	var out []scored
	for i, it := range items {
		hay := strings.ToLower(it.label + " " + it.id + " " + it.tag + " " + it.desc)
		total, ok := 0, true
		for _, tok := range tokens {
			s, matched := fuzzyScore(tok, hay)
			if !matched {
				ok = false
				break
			}
			total += s
		}
		if ok {
			out = append(out, scored{it: it, score: total, idx: i})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].score != out[j].score {
			return out[i].score > out[j].score
		}
		return out[i].idx < out[j].idx
	})
	result := make([]pickItem, len(out))
	for i, s := range out {
		result[i] = s.it
	}
	return result
}

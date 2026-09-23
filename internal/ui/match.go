package ui

// The fuzzy matcher is fzf's own (tech-design §9.7). src/algo is not an API
// fzf promises to keep, so go.mod pins the version and this file is the only
// place that imports it.

import (
	"slices"
	"strings"
	"unicode/utf8"

	"github.com/junegunn/fzf/src/algo"
	"github.com/junegunn/fzf/src/util"
)

func init() { algo.Init("default") } // fzf's default scoring scheme

// Match is an item that passed the pattern.
type Match struct {
	Index int   // into the items given to Filter
	Score int   // fzf's score, summed over the terms
	Pos   []int // rune offsets to highlight, ascending
}

type term struct {
	match     algo.Algo
	inverse   bool
	text      []rune
	sensitive bool // smartcase: the term has an upper-case letter
	normalize bool // fold accents, unless the term has some itself
}

// parseTerms reads the common part of fzf's extended syntax (§9.7): terms
// split by spaces must all match; 'exact, ^prefix, suffix$ and !exclude.
// ponytail: no `|` (OR) and no 'exact-boundary'; add them the way fzf's
// pattern.go does if lists grow long enough to need them.
func parseTerms(pattern string) []term {
	var terms []term
	for _, s := range strings.Fields(pattern) {
		lower := strings.ToLower(s)
		t := term{match: algo.FuzzyMatchV2, sensitive: s != lower}
		t.normalize = lower == string(algo.NormalizeRunes([]rune(lower)))
		if !t.sensitive {
			s = lower
		}
		if strings.HasPrefix(s, "!") {
			t.inverse, t.match, s = true, algo.ExactMatchNaive, s[1:]
		}
		suffix := s != "$" && strings.HasSuffix(s, "$")
		if suffix {
			t.match, s = algo.SuffixMatch, s[:len(s)-1]
		}
		switch {
		case strings.HasPrefix(s, "'"): // flips exactness, as in fzf
			t.match, s = algo.ExactMatchNaive, s[1:]
			if t.inverse {
				t.match = algo.FuzzyMatchV2
			}
		case strings.HasPrefix(s, "^"):
			t.match, s = algo.PrefixMatch, s[1:]
			if suffix {
				t.match = algo.EqualMatch
			}
		}
		if s == "" {
			continue
		}
		t.text = []rune(s)
		if t.normalize {
			t.text = algo.NormalizeRunes(t.text)
		}
		terms = append(terms, t)
	}
	return terms
}

// Filter returns the items that match pattern, ranked as fzf ranks them: by
// score, then the shorter item, then the original order. An empty pattern
// keeps every item, in order.
func Filter(pattern string, items []string) []Match {
	terms := parseTerms(pattern)
	var out []Match
	if len(terms) == 0 {
		for i := range items {
			out = append(out, Match{Index: i})
		}
		return out
	}
	for i, s := range items {
		if m, ok := matchTerms(terms, s); ok {
			m.Index = i
			out = append(out, m)
		}
	}
	slices.SortStableFunc(out, func(a, b Match) int {
		if a.Score != b.Score {
			return b.Score - a.Score
		}
		return utf8.RuneCountInString(items[a.Index]) - utf8.RuneCountInString(items[b.Index])
	})
	return out
}

func matchTerms(terms []term, s string) (Match, bool) {
	var m Match
	chars := util.ToChars([]byte(s))
	for _, t := range terms {
		res, pos := t.match(t.sensitive, t.normalize, true, &chars, t.text, true, nil)
		if found := res.Start >= 0; found == t.inverse {
			return Match{}, false
		}
		if t.inverse {
			continue
		}
		m.Score += res.Score
		if pos != nil {
			m.Pos = append(m.Pos, *pos...)
		} else { // exact kinds report a range
			for i := res.Start; i < res.End; i++ {
				m.Pos = append(m.Pos, i)
			}
		}
	}
	slices.Sort(m.Pos)
	m.Pos = slices.Compact(m.Pos)
	return m, true
}

package doctor

import (
	"sort"
	"strings"
	"unicode"
)

var stopwords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a about after again all also am an and any anymore are as at be been but by can cant
		computer could did didnt do does doesnt dont for from get gets getting go had has have help how i im is isnt it its
		ive just keep keeps laptop linux me my no not of on or pc please problem randomly since so some sometimes still
		suddenly that the there this to too ubuntu up very was wasnt what when where which while why will with wont work
		working works would`) {
		stopwords[w] = true
	}
}

// Words splits a question into lowercase search words, joining "wi-fi" into
// "wifi" and dropping filler like "why", "my" and "doesn't".
func Words(q string) []string {
	q = strings.ToLower(q)
	q = strings.NewReplacer("-", "", "'", "", "’", "").Replace(q)
	var out []string
	for _, w := range strings.FieldsFunc(q, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) }) {
		if !stopwords[w] {
			out = append(out, w)
		}
	}
	return out
}

// related matches a search word against a known word, allowing for word
// endings: "charging" finds "charge", "freez" (still typing) finds "freeze".
func related(word, known string) bool {
	if word == known {
		return true
	}
	if len(word) >= 2 && strings.HasPrefix(known, word) {
		return true
	}
	stem := min(len(known), len(word), max(4, len(known)-2))
	return len(known) >= 4 && len(word) >= 4 && word[:stem] == known[:stem]
}

// Score says how well a check matches a question in the user's own words.
// Keywords weigh more than words from the check's name and question.
func Score(c Check, q string) int {
	text := Words(c.Name + " " + c.Question + " " + c.Category)
	score := 0
	for _, w := range Words(q) {
		best := 0
		for _, k := range c.Keywords {
			if related(w, k) {
				best = 3
				break
			}
		}
		if best == 0 {
			for _, k := range text {
				if related(w, k) {
					best = 1
					break
				}
			}
		}
		score += best
	}
	return score
}

// Match returns the checks relevant to a question, best first. Checks that
// score far below the best one are dropped so "no sound in zoom" doesn't drag
// in every check that mentions "in".
func Match(cs []Check, q string) []Check {
	type scored struct {
		c Check
		n int
	}
	var ss []scored
	for _, c := range cs {
		if n := Score(c, q); n > 0 {
			ss = append(ss, scored{c, n})
		}
	}
	sort.SliceStable(ss, func(i, j int) bool { return ss[i].n > ss[j].n })
	var out []Check
	for _, s := range ss {
		if s.n*2 >= ss[0].n {
			out = append(out, s.c)
		}
	}
	return out
}

package search

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"testing"
	"unicode"
)

// tokenSignature ignores case, punctuation and word order, but preserves every
// token and its multiplicity. It deliberately does not infer paraphrases.
func tokenSignature(s string) string {
	words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
	sort.Strings(words)
	return strings.Join(words, " ")
}

// nearCopy requires identical ordered substantive tokens (including numbers and
// negations) and cosine >= 0.98. Stopword-only variation is not semantic proof.
func nearCopy(a, b string, av, bv []float32) bool {
	if len(av) == 0 || len(av) != len(bv) {
		return false
	}
	stop := map[string]bool{"a": true, "an": true, "the": true}
	tokens := func(s string) []string {
		words := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsNumber(r) })
		out := []string{}
		for _, word := range words {
			if !stop[word] {
				out = append(out, word)
			}
		}
		return out
	}
	x, y := tokens(a), tokens(b)
	if len(x) < 3 || len(x) != len(y) {
		return false
	}
	for i := range x {
		if x[i] != y[i] {
			return false
		}
	}
	var dot, aa, bb float64
	for i := range av {
		u, v := float64(av[i]), float64(bv[i])
		dot += u * v
		aa += u * u
		bb += v * v
	}
	return aa > 0 && bb > 0 && dot/math.Sqrt(aa*bb) >= 0.98
}

func selectGoldenDocs(docs []goldenDoc, limit int, mode string, vectors map[string][]float32) []goldenDoc {
	out := make([]goldenDoc, 0, limit)
	seen := map[string]bool{}
	for _, doc := range docs {
		if len(out) == limit {
			break
		}
		key := tokenSignature(doc.Text)
		duplicate := mode == "multiset" && key != "" && seen[key]
		if mode == "near" {
			for _, retained := range out {
				if nearCopy(doc.Text, retained.Text, vectors[doc.ID], vectors[retained.ID]) {
					duplicate = true
					break
				}
			}
		}
		if duplicate {
			continue
		}
		seen[key] = true
		out = append(out, doc)
	}
	return out
}

type selectionTotals struct {
	queries, eligible, all, groupHits, groups, slots, noise, redundancy, stale, scope, empty, staleFirst, unanswerable, unanswerableResults int
	recall                                                                                                                                  float64
}

func (a *selectionTotals) add(c goldenCase, docs []goldenDoc) {
	a.queries++
	if len(docs) == 0 {
		a.empty++
	}
	if len(docs) > 0 && docs[0].Stale {
		a.staleFirst++
	}
	if len(c.Groups) == 0 {
		a.unanswerable++
		if len(docs) > 0 {
			a.unanswerableResults++
		}
	}
	matched := make([]bool, len(c.Groups))
	seen := map[string]bool{}
	for _, doc := range docs {
		a.slots++
		relevant := false
		for i, group := range c.Groups {
			for _, id := range group {
				if id == doc.ID {
					matched[i] = true
					relevant = true
				}
			}
		}
		if !relevant {
			a.noise++
		}
		if seen[doc.Group] {
			a.redundancy++
		}
		seen[doc.Group] = true
		if doc.Stale {
			a.stale++
		}
		if doc.Workspace != "" {
			a.scope++
		}
	}
	if len(c.Groups) > 0 {
		a.eligible++
		n := 0
		for _, hit := range matched {
			if hit {
				n++
			}
		}
		a.groupHits += n
		a.groups += len(matched)
		a.recall += float64(n) / float64(len(matched))
		if n == len(matched) {
			a.all++
		}
	}
}
func (a *selectionTotals) report(split, variant string) string {
	recall := "N/A"
	if a.eligible > 0 {
		recall = fmt.Sprintf("%.3f", a.recall/float64(a.eligible))
	}
	return fmt.Sprintf("SELECTION split=%s variant=%s queries=%d all=%d/%d recall=%s group_hits=%d/%d noise=%d/%d redundancy=%d/%d stale=%d/%d scope=%d/%d empty=%d/%d stale_at_1=%d/%d unanswerable_with_results=%d/%d", split, variant, a.queries, a.all, a.eligible, recall, a.groupHits, a.groups, a.noise, a.slots, a.redundancy, a.slots, a.stale, a.slots, a.scope, a.slots, a.empty, a.queries, a.staleFirst, a.queries, a.unanswerableResults, a.unanswerable)
}

func TestGoldenSelectionMetrics(t *testing.T) {
	c := goldenCase{Groups: [][]string{{"a", "b"}, {"c"}}}
	docs := []goldenDoc{{ID: "a", Group: "one", Stale: true}, {ID: "b", Group: "one"}, {ID: "x", Group: "other", Workspace: "foreign"}}
	var a selectionTotals
	a.add(c, docs)
	a.add(goldenCase{}, nil)
	a.add(goldenCase{}, docs[:1])
	if a.queries != 3 || a.eligible != 1 || a.all != 0 || a.groupHits != 1 || a.groups != 2 || a.recall != .5 || a.noise != 2 || a.redundancy != 1 || a.stale != 2 || a.staleFirst != 2 || a.scope != 1 || a.empty != 1 || a.unanswerable != 2 || a.unanswerableResults != 1 {
		t.Fatalf("unexpected metrics: %+v", a)
	}
}

func TestGoldenNearCopy(t *testing.T) {
	v := []float32{1, 0}
	for _, tc := range []struct {
		name, a, b string
		vector     []float32
		want       bool
	}{
		{"near", "the service uses JWT tokens", "service uses a JWT tokens", v, true},
		{"different vector", "the service uses JWT tokens", "service uses JWT tokens", []float32{0, 1}, false},
		{"number", "service retains 30 days", "service retains 90 days", v, false},
		{"negation", "service allows JWT tokens", "service does not allow JWT tokens", v, false},
		{"reversal", "alice approves bob requests", "bob approves alice requests", v, false},
		{"missing vectors", "service uses JWT tokens", "service uses JWT tokens", nil, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := nearCopy(tc.a, tc.b, v, tc.vector); got != tc.want {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
	docs := []goldenDoc{{ID: "a", Text: "the service uses JWT tokens"}, {ID: "b", Text: "service uses JWT tokens"}, {ID: "c", Text: "service uses session tokens"}, {ID: "d", Text: "other useful document"}}
	got := selectGoldenDocs(docs, 3, "near", map[string][]float32{"a": v, "b": v, "c": v, "d": v})
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "c" || got[2].ID != "d" {
		t.Fatalf("near-copy selection: %+v", got)
	}
}

func TestGoldenSelection(t *testing.T) {
	docs := []goldenDoc{{ID: "a", Text: "Service uses JWT tokens."}, {ID: "b", Text: "JWT tokens: service uses!"}, {ID: "c", Text: "Service uses session tokens."}, {ID: "d", Text: "Service uses JWT tokens in production."}}
	got := selectGoldenDocs(docs, 3, "multiset", nil)
	if len(got) != 3 || got[0].ID != "a" || got[1].ID != "c" || got[2].ID != "d" {
		t.Fatalf("rank retention / distinct facts: %+v", got)
	}
	raw := selectGoldenDocs(docs, 3, "raw", nil)
	if len(raw) != 3 || raw[1].ID != "b" || raw[2].ID != "c" {
		t.Fatalf("raw rank changed: %+v", raw)
	}
	if len(selectGoldenDocs(docs, 0, "multiset", nil)) != 0 {
		t.Fatal("zero limit")
	}
}

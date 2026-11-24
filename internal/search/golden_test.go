package search

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/Syfra3/ancora/internal/embed"
	"github.com/Syfra3/ancora/internal/embedding"
	"github.com/Syfra3/ancora/internal/store"
)

type goldenDoc struct {
	ID, Text, Group, Workspace string
	Stale                      bool
}
type goldenCase struct {
	ID, Split, Category, Question, Expected string
	Groups                                  [][]string
}
type goldenData struct {
	CorpusID  string `json:"corpus_id"`
	Documents []goldenDoc
	Cases     []goldenCase
}
type goldenTotals struct {
	queries, eligible, all, slots, noise, redundant, stale, scope, empty int
	recall, rr                                                           float64
	ms, embedMS, otherMS                                                 []float64
}

// Keep native identity checks in the search path. Time only the CLI call;
// the remaining wall time includes identity hashing, SQLite, and fusion.
type goldenTimedEmbedder struct {
	model   *embed.NomicEmbedder
	elapsed chan time.Duration
}

func (e goldenTimedEmbedder) EmbeddingSpec() store.EmbeddingSpec {
	spec, _ := embedding.ResolveSpec(e.model)
	return spec
}

func (e goldenTimedEmbedder) Embed(text string) ([]float32, error) {
	start := time.Now()
	vec, err := e.model.Embed(text)
	e.elapsed <- time.Since(start)
	return vec, err
}

func loadGolden(t *testing.T) goldenData {
	t.Helper()
	b, err := os.ReadFile("testdata/golden-v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var d goldenData
	if err := json.Unmarshal(b, &d); err != nil {
		t.Fatal(err)
	}
	if d.CorpusID == "" {
		t.Fatal("missing corpus id")
	}
	docs := map[string]goldenDoc{}
	for _, doc := range d.Documents {
		if doc.ID == "" || doc.Text == "" || doc.Group == "" {
			t.Fatal("invalid document")
		}
		if _, ok := docs[doc.ID]; ok {
			t.Fatal("duplicate document", doc.ID)
		}
		docs[doc.ID] = doc
	}
	cases := map[string]bool{}
	counts := map[string]int{}
	categories := map[string]bool{"exact": true, "paraphrase": true, "multi": true, "stale": true, "unanswerable": true}
	for _, c := range d.Cases {
		if c.ID == "" || c.Question == "" || c.Expected == "" || !categories[c.Category] || cases[c.ID] || (c.Split != "dev" && c.Split != "holdout") {
			t.Fatal("invalid case", c.ID)
		}
		cases[c.ID] = true
		counts[c.Split+"/"+c.Category]++
		if (c.Category == "unanswerable") != (len(c.Groups) == 0) {
			t.Fatal("invalid answerability", c.ID)
		}
		for _, group := range c.Groups {
			if len(group) == 0 {
				t.Fatal("empty evidence group", c.ID)
			}
			for _, id := range group {
				doc, ok := docs[id]
				if !ok || doc.Stale || doc.Workspace != "" {
					t.Fatal("invalid evidence", c.ID, id)
				}
			}
		}
	}
	for _, cat := range []string{"exact", "paraphrase", "multi", "stale", "unanswerable"} {
		if counts["dev/"+cat] != 8 {
			t.Fatal("wrong dev count", cat, counts)
		}
	}
	if len(d.Cases) != 44 {
		t.Fatal("wrong total count")
	}
	return d
}

func percentile(xs []float64, p float64) float64 {
	if len(xs) == 0 {
		return 0
	}
	v := append([]float64(nil), xs...)
	sort.Float64s(v)
	return v[int(math.Ceil(p*float64(len(v))))-1]
}

func TestGoldenRetrieval(t *testing.T) {
	d := loadGolden(t)
	hybrid := os.Getenv("ANCORA_GOLDEN_HYBRID") == "1"
	cfg, err := store.DefaultConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.DataDir = t.TempDir()
	s, err := store.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err = s.CreateSession("golden", "golden", ""); err != nil {
		t.Fatal(err)
	}
	var model *embed.NomicEmbedder
	var spec store.EmbeddingSpec
	if hybrid {
		model, err = embed.New()
		if err != nil {
			t.Fatal(err)
		}
		spec, err = embedding.ResolveSpec(model)
		if err != nil {
			t.Fatal(err)
		}
		t.Logf("MODEL identity=%s preprocessing=%s dimensions=%d", spec.Model, spec.Preprocessing, spec.Dimensions)
		if err = s.ConfigureEmbedding(spec); err != nil {
			t.Fatal(err)
		}
	}
	byID := map[int64]goldenDoc{}
	byName := map[string]goldenDoc{}
	vectors := map[string][]float32{}
	for _, doc := range d.Documents {
		workspace := doc.Workspace
		if workspace == "" {
			workspace = "golden"
		}
		id, e := s.AddObservation(store.AddObservationParams{SessionID: "golden", Title: doc.ID, Content: doc.Text, Workspace: workspace, Visibility: "work", Organization: "fixture", Type: "decision"})
		if e != nil {
			t.Fatal(e)
		}
		byID[id] = doc
		byName[doc.ID] = doc
		if hybrid {
			vec, e := model.Embed(doc.ID + ". " + doc.Text)
			if e != nil {
				t.Fatalf("document %s: %v", doc.ID, e)
			}
			vectors[doc.ID] = vec
			now := time.Now()
			job, e := s.LeaseEmbedding(spec, now, time.Minute)
			if e != nil || job == nil || job.ObservationID != id {
				t.Fatalf("embedding job for %s: %+v %v", doc.ID, job, e)
			}
			if e = s.CompleteEmbedding(*job, vec, now); e != nil {
				t.Fatal(e)
			}
		}
	}
	opts := store.SearchOptions{Workspace: "golden", Visibility: "work", Organization: "fixture", Type: "decision", Limit: 5}
	totals := map[string]*goldenTotals{}
	selection := map[string]*selectionTotals{}
	fused := 0
	for _, c := range d.Cases {
		var emb Embedder
		var embeddingElapsed chan time.Duration
		if hybrid {
			embeddingElapsed = make(chan time.Duration, 1)
			emb = goldenTimedEmbedder{model, embeddingElapsed}
		}
		start := time.Now()
		results, mode, e := SearchWithOptions(c.Question, opts, emb, s)
		elapsed := float64(time.Since(start)) / float64(time.Millisecond)
		if e != nil {
			t.Fatal(e)
		}
		if hybrid && mode == ModeKeyword {
			t.Fatalf("%s: native embedding fell back to keyword", c.ID)
		}
		if mode == ModeHybrid {
			fused++
		}
		key := c.Split + "/" + c.Category
		a := totals[key]
		if a == nil {
			a = &goldenTotals{}
			totals[key] = a
		}
		a.queries++
		a.ms = append(a.ms, elapsed)
		embedMS := 0.0
		if hybrid {
			embedMS = float64(<-embeddingElapsed) / float64(time.Millisecond)
			a.embedMS = append(a.embedMS, embedMS)
		}
		otherMS := elapsed - embedMS
		a.otherMS = append(a.otherMS, otherMS)
		if len(results) == 0 {
			a.empty++
		}
		relevant := map[string]bool{}
		for _, g := range c.Groups {
			for _, id := range g {
				relevant[id] = true
			}
		}
		seen := map[string]bool{}
		ranked := []string{}
		scores := []float64{}
		matched := map[int]bool{}
		rr := 0.0
		for i, r := range results {
			doc, ok := byID[r.ID]
			if !ok {
				t.Fatalf("unknown result %d", r.ID)
			}
			ranked = append(ranked, doc.ID)
			scores = append(scores, r.Rank)
			a.slots++
			if !relevant[doc.ID] {
				a.noise++
			}
			if seen[doc.Group] {
				a.redundant++
			}
			seen[doc.Group] = true
			if doc.Stale {
				a.stale++
			}
			if r.Workspace == nil || *r.Workspace != "golden" || r.Visibility != "work" || r.Organization == nil || *r.Organization != "fixture" || r.Type != "decision" {
				a.scope++
			}
			for gi, g := range c.Groups {
				for _, id := range g {
					if id == doc.ID {
						matched[gi] = true
						if rr == 0 {
							rr = 1 / float64(i+1)
						}
					}
				}
			}
		}
		if os.Getenv("ANCORA_GOLDEN_SELECTION") == "1" {
			docs := make([]goldenDoc, 0, len(results))
			for _, r := range results {
				doc := byID[r.ID]
				if r.Workspace == nil || *r.Workspace != "golden" || r.Visibility != "work" || r.Organization == nil || *r.Organization != "fixture" || r.Type != "decision" {
					doc.Workspace = "out-of-scope"
				}
				docs = append(docs, doc)
			}
			for _, variant := range []struct {
				name  string
				limit int
				mode  string
			}{{"raw5", 5, "raw"}, {"raw4", 4, "raw"}, {"raw3", 3, "raw"}, {"multiset3", 3, "multiset"}, {"near3", 3, "near"}} {
				key := c.Split + "/" + variant.name
				if selection[key] == nil {
					selection[key] = &selectionTotals{}
				}
				selected := selectGoldenDocs(docs, variant.limit, variant.mode, vectors)
				selection[key].add(c, selected)
				ids := make([]string, 0, len(selected))
				for _, doc := range selected {
					ids = append(ids, doc.ID)
				}
				t.Logf("SELECTION_QUERY id=%s variant=%s ranked=%v", c.ID, variant.name, ids)
			}
		}
		if len(c.Groups) > 0 {
			a.eligible++
			a.recall += float64(len(matched)) / float64(len(c.Groups))
			a.rr += rr
			if len(matched) == len(c.Groups) {
				a.all++
			}
		}
		t.Logf("QUERY corpus=%s split=%s category=%s id=%s mode=%s ranked=%v scores=%v groups=%v latency_ms=%.3f embedding_call_ms=%.3f other_ms=%.3f", d.CorpusID, c.Split, c.Category, c.ID, mode, ranked, scores, c.Groups, elapsed, embedMS, otherMS)
		if os.Getenv("ANCORA_GOLDEN_CONTEXT") == "1" {
			type item struct {
				ID   string `json:"id"`
				Text string `json:"text"`
			}
			retrieved := []item{}
			oracle := []item{}
			for _, id := range ranked {
				retrieved = append(retrieved, item{id, byName[id].Text})
			}
			for _, g := range c.Groups {
				id := g[0]
				oracle = append(oracle, item{id, byName[id].Text})
			}
			row := map[string]any{"corpus_id": d.CorpusID, "id": c.ID, "split": c.Split, "question": c.Question, "expected": c.Expected, "retrieved_context": retrieved, "oracle_context": oracle}
			b, err := json.Marshal(row)
			if err != nil {
				t.Fatal(err)
			}
			fmt.Printf("CONTEXT %s\n", b)
		}
	}
	if hybrid && fused == 0 {
		t.Fatal("native run never exercised fused hybrid retrieval")
	}
	if os.Getenv("ANCORA_GOLDEN_SELECTION") == "1" {
		for _, split := range []string{"dev", "holdout"} {
			for _, variant := range []string{"raw5", "raw4", "raw3", "multiset3", "near3"} {
				t.Log(selection[split+"/"+variant].report(split, variant))
			}
		}
	}
	for _, split := range []string{"dev", "holdout"} {
		total := &goldenTotals{}
		for _, category := range []string{"exact", "paraphrase", "multi", "stale", "unanswerable"} {
			a := totals[split+"/"+category]
			if a == nil {
				continue
			}
			total.queries += a.queries
			total.eligible += a.eligible
			total.all += a.all
			total.slots += a.slots
			total.noise += a.noise
			total.redundant += a.redundant
			total.stale += a.stale
			total.scope += a.scope
			total.empty += a.empty
			total.recall += a.recall
			total.rr += a.rr
			total.ms = append(total.ms, a.ms...)
			total.embedMS = append(total.embedMS, a.embedMS...)
			total.otherMS = append(total.otherMS, a.otherMS...)
		}
		totals[split+"/all"] = total
	}
	keys := make([]string, 0, len(totals))
	for key := range totals {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		a := totals[key]
		rec, mrr, all := "N/A", "N/A", "N/A"
		if a.eligible > 0 {
			rec = fmt.Sprintf("%.3f", a.recall/float64(a.eligible))
			mrr = fmt.Sprintf("%.3f", a.rr/float64(a.eligible))
			all = evalRate(a.all, a.eligible)
		}
		t.Logf("SUMMARY %s queries=%d eligible=%d recall@5=%s mrr@5=%s all_required=%d/%d(%s) noise=%d/%d(%s) redundancy=%d/%d(%s) stale=%d/%d(%s) scope=%d/%d(%s) empty=%d/%d(%s) latency_ms_p50=%.3f p95=%.3f p99=%.3f", key, a.queries, a.eligible, rec, mrr, a.all, a.eligible, all, a.noise, a.slots, evalRate(a.noise, a.slots), a.redundant, a.slots, evalRate(a.redundant, a.slots), a.stale, a.slots, evalRate(a.stale, a.slots), a.scope, a.slots, evalRate(a.scope, a.slots), a.empty, a.queries, evalRate(a.empty, a.queries), percentile(a.ms, .5), percentile(a.ms, .95), percentile(a.ms, .99))
		if len(a.embedMS) > 0 {
			t.Logf("LATENCY_SPLIT %s embedding_call_ms_p50=%.3f p95=%.3f other_ms_p50=%.3f p95=%.3f", key, percentile(a.embedMS, .5), percentile(a.embedMS, .95), percentile(a.otherMS, .5), percentile(a.otherMS, .95))
		}
		if a.scope != 0 {
			t.Errorf("scope violation in %s", key)
		}
	}
}

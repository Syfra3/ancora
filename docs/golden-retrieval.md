# Golden retrieval v2

`internal/search/testdata/golden-v2.json` is a shareable, fictional, hand-authored fixture. It has 40 development questions (eight each exact, paraphrase, multi-evidence, stale and unanswerable), four separate holdout questions, stable case/document IDs, expected answers and alternative evidence IDs grouped by required fact. The holdout shares the corpus and is small: it is not a statistically independent benchmark. Stale, duplicate and foreign-workspace documents are deliberate distractors. Version the corpus ID when changing its meaning; do not treat this toy dataset as production quality evidence.

From the repository root, using cached Go dependencies:

```sh
GOPROXY=off GOTOOLCHAIN=local GOSUMDB=off make eval-golden
GOPROXY=off GOTOOLCHAIN=local GOSUMDB=off ANCORA_GOLDEN_HYBRID=1 make eval-golden
```

The second command explicitly opts into the real native Nomic GGUF and local llama embedding CLI resolved by `embed.New`; it does not download a model. It fails if initialization or document embedding fails, if any query silently falls back to keyword, or if no query exercises fused hybrid. Semantic-only is legitimate when the keyword side is empty and is reported per query. Both runs build a fresh temporary SQLite store, never the live database. `QUERY` rows contain ranked stable IDs, native scores (not comparable across search modes), mode and elapsed milliseconds; `SUMMARY` rows are grouped by split and category.

Recall@5 averages the fraction of required evidence groups hit (any alternative suffices); MRR@5 is the reciprocal rank of the first relevant ID, averaged over answerable cases. All-required is the fraction of answerable cases with every group hit. These three are N/A on unanswerable cases. Noise counts returned slots not labeled relevant, redundancy counts slots repeating a document group, stale counts stale slots, and scope counts out-of-scope slots; all use returned slots as denominator. Empty is the fraction of queries with zero results. Latencies are nearest-rank p50/p95/p99 per split/category, including query embedding and search but excluding fixture setup. For native runs, the split reports embedding-call wall time (including setup, subprocess execution and parsing) and the remaining time (including SQLite search, fusion and model identity hashing). Document indexing is excluded. Each embedding invocation starts a subprocess; OS caches are not controlled, so these are not separate cold and warm measurements. Tiny groups make percentiles descriptive only. Unanswerable queries count all returned slots as noise; retrieval is not an abstention decision. A zero-slot denominator is N/A.

Set `ANCORA_GOLDEN_CONTEXT=1` to print `CONTEXT` JSON rows with retrieved and oracle context items plus reference answer; then use the [paired generation exporter](golden-generation.md) to compare prompts. Oracle selects the first labeled alternative per required group. Keyword search currently ANDs sanitized query terms, so full natural-language questions can yield no results; that is a measured property of the actual search path, not a transformed-query benchmark.

## Opt-in context selection comparison

Set `ANCORA_GOLDEN_SELECTION=1` alongside either command above to report `SELECTION` rows. Every variant starts from the same actual top-five ranking. `raw5`, `raw4` and `raw3` take a prefix. `multiset3` suppresses identical nonempty lowercase token multisets before taking three; ignoring word order can merge distinct facts. `near3` instead requires the same ordered tokens after removing `a`, `an` and `the`, at least three remaining tokens, and document-vector cosine similarity of at least 0.98. It reuses vectors already available in the Nomic run and does not deduplicate without vectors. Neither rule establishes semantic equivalence. Labels are used only for scoring, no additional candidates are fetched, and search and prompt export are unchanged.

Recall is the mean fraction of required groups hit over answerable queries; group_hits is the pooled count, and all is answerable queries with every group. Noise, label-based redundancy, stale and scope are counts per returned slot. Empty and stale_at_1 are per query; unanswerable_with_results is per unanswerable query. A zero denominator is undefined, not zero percent. The control `QUERY`, `SUMMARY`, and `CONTEXT` behavior remains unchanged.

Local observation at dirty HEAD `4b06751`, fixture SHA `17b3adbbd779`, cached Nomic and `GGML_VK_VISIBLE_DEVICES='' OMP_NUM_THREADS=2`. Each cell is count/denominator; recall is the mean above. Keyword rows are identical across variants. Neither dedup rule improved measured results over raw3; all three lost one multi-evidence development case. Raw4 retained all required evidence in both splits while returning 20% fewer slots. This did not improve stale-at-1 or unanswerable results; it does not establish a token savings, retrieval speedup, or deployment readiness.

| Mode/split/variant | all | recall (hits) | noise | redundancy | stale | scope | empty | stale@1 | unanswerable results |
|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| Keyword dev, all variants | 8/32 | .250 (8/40) | 8/17 | 1/17 | 8/17 | 0/17 | 32/40 | 0/40 | 0/8 |
| Keyword holdout, all variants | 0/3 | .000 (0/4) | 0/0 | 0/0 | 0/0 | 0/0 | 4/4 | 0/4 | 0/1 |
| Native dev raw5 | 32/32 | 1.000 (40/40) | 155/200 | 23/200 | 56/200 | 0/200 | 0/40 | 8/40 | 8/8 |
| Native dev raw4 | 32/32 | 1.000 (40/40) | 115/160 | 14/160 | 40/160 | 0/160 | 0/40 | 8/40 | 8/8 |
| Native dev raw3, multiset3, near3 | 31/32 | .984 (39/40) | 76/120 | 10/120 | 31/120 | 0/120 | 0/40 | 8/40 | 8/8 |
| Native holdout raw5 | 3/3 | 1.000 (4/4) | 15/20 | 3/20 | 3/20 | 0/20 | 0/4 | 0/4 | 1/1 |
| Native holdout raw4 | 3/3 | 1.000 (4/4) | 11/16 | 3/16 | 3/16 | 0/16 | 0/4 | 0/4 | 1/1 |
| Native holdout raw3, multiset3, near3 | 3/3 | 1.000 (4/4) | 8/12 | 2/12 | 2/12 | 0/12 | 0/4 | 0/4 | 1/1 |

Earlier native controls reported stale-at-1 between 7/40 and 8/40, so compare candidates from the same run. These measurements describe the small fictional fixture, not generalization or generation quality.

## Initial local observation

On the uncommitted `feat/harness-context-reliability` checkout at HEAD `4b06751`, Go 1.27.2/Linux amd64, Intel Core i9-14900K, llama.cpp build 8670, Nomic model/CLI identity `70fe3f7d9e5519654ad359e5d3f93e7b77e7b4e33aa5f0710f701e916785df0b`. The native run used `GGML_VK_VISIBLE_DEVICES='' OMP_NUM_THREADS=2`. Corpus SHA-256: `17b3adbbd779e29ddd16944ffa2139308edfa52bf64b9653b04e97d8d42fca61`.

| Development split | Recall@5 | MRR@5 | All evidence | Noise | Redundancy | Stale | Empty | p50 / p95 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| Keyword | 0.250 | 0.250 | 8/32 | 8/17 | 1/17 | 8/17 | 32/40 | 0.52 / 0.66 ms |
| Native hybrid | 1.000 | 0.891 | 32/32 | 155/200 | 23/200 | 56/200 | 0/40 | 257 / 270 ms |

Both runs had zero scope violations. Holdout all-evidence was 0/3 for keyword and 3/3 for native hybrid. The native development p50 included about 181 ms in the embedding call. This is a single observation, not a release gate. For comparison, use a clean commit, the same corpus digest, model/CLI and machine settings, and retain multiple raw runs.

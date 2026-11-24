# Offline retrieval baseline: dirty checkout

Captured 2026-11-24 UTC, from the repository root. This is an illustrative local baseline, not a release gate or a threshold. Raw outputs are [`eval.txt`](eval.txt) and benchstat-compatible [`bench.txt`](bench.txt); [`fixture.go.txt`](fixture.go.txt) is a byte-for-byte copy of the test/fixture used. No production code was changed for this capture.

## Identity and reproducibility

- Branch `feat/harness-context-reliability`, HEAD `4b06751a59203ca70cf44cb2f46efc71f7ba2741`; checkout **dirty**, including tracked modifications and untracked source. Before these artifacts, `git status --short` listed modified `Makefile`, `README.md`, `internal/embedding/service.go`, `internal/mcp/mcp.go`, `internal/search/search.go`, `internal/search/search_test.go`, `internal/store/integration_test.go`, `internal/store/store.go`, and `internal/store/store_test.go`; untracked `.rotta/`, `.vela/`, `internal/embedding/harness_recovery_test.go`, `internal/embedding/harness_reliability_test.go`, `internal/mcp/harness_reliability_test.go`, `internal/search/eval_test.go`, `internal/search/harness_reliability_test.go`, `internal/store/embedding_jobs.go`, `internal/store/harness_concurrency_test.go`, and `internal/store/harness_reliability_test.go`. This artifact directory was then added as untracked.
- Relevant source fingerprint SHA-256: `3bfeb456c783e7a7158b6a75c07d2c0608c9c9585300a564f02e5f9ec418493a` (64 paths). It covers existing tracked and non-ignored untracked paths under `internal/`, plus `go.mod`, `go.sum`, and `Makefile`. This includes migrations wherever located under `internal/`. Recompute from the root with GNU tools:

  ```sh
  git ls-files -c -o --exclude-standard -z -- internal/ go.mod go.sum Makefile |
    LC_ALL=C sort -zu | xargs -0 sha256sum | sha256sum
  ```

  This hashes sorted path-and-content-hash lines; renames and content changes alter it. It identifies the selected working-tree bytes, **not** ignored files, external dependencies, or the complete checkout; it does **not archive** the uncommitted branch. The fixture copy archives only the fixture, not its dependencies. Preserve the source separately and use a clean commit for the first release-grade baseline.
- Fixture SHA-256: `647d599436eca3a258bb6130e018a9470c5595a4b3268db04f1903a782cc0688` for both `internal/search/eval_test.go` and `fixture.go.txt` at capture.
- Go `go1.27.2 linux/amd64`; `GOOS=linux`, `GOARCH=amd64`, `GOAMD64=v1`, `CGO_ENABLED=1`; Linux `7.2.11-arch1-1 x86_64`; Intel Core i9-14900K, 32 online logical CPUs. The `-32` benchmark suffix reflects Go's parallelism setting, not 32 concurrent benchmark workers. CPU scaling and other host activity were not controlled.

## Capture and comparison

From the repository root, with pinned modules already cached, run sequentially (not alongside heavy tests):

```sh
GOPROXY=off GOTOOLCHAIN=local GOSUMDB=off make eval-retrieval > eval-new.txt 2>&1
GOPROXY=off GOTOOLCHAIN=local GOSUMDB=off go test ./internal/search -run '^$' -bench '^BenchmarkRetrievalEval$' -benchmem -benchtime=1s -count=6 > bench-new.txt 2>&1
```

Check that both commands exit successfully and the logs show `PASS`. Compare on the same machine, toolchain, settings and as similar a load as possible; confirm the same `-32` benchmark suffix. Check the fixture hash before each run; use the exact archived fixture and parameters for both versions. If the fixture is missing or changed, restore the archived bytes as `internal/search/eval_test.go` in a suitable isolated checkout after preserving any existing file. A later code version may require adapting the harness; any adaptation changes the fixture and must be disclosed as a non-identical comparison. Preserve both raw outputs. Compare quality counts and per-query rankings as well as timings and allocation medians/ranges. If `benchstat` is already installed, use `benchstat docs/retrieval-baseline/bench.txt bench-new.txt`; it was not installed at capture, and no installation is required. Otherwise compare medians and spread directly; six samples are not a guarantee against noise or order effects.

## Observed quality

| Variant | Recall@5 | MRR@5 | Noise / slots | Redundant / slots | Empty / queries | Scope violations |
|---|---:|---:|---:|---:|---:|---:|
| keyword | 0.333 | 0.333 | 0/2 | 1/2 | 3/4 | 0 |
| synthetic-hybrid | 1.000 | 1.000 | 12/20 | 4/20 | 0/4 | 0 |
| error-fallback | 0.333 | 0.333 | 0/2 | 1/2 | 3/4 | 0 |

Three queries have labeled relevant results; one does not, and hybrid returned results for that one. Recall and reciprocal rank exclude that query; noise and redundancy count returned slots, including that query, and empties count all four queries. Fallback parity is asserted by the test. See raw output for per-query rankings and counts.

## Observed performance

Six samples per configuration. Values are median [minimum, maximum], computed from the raw rows; even-sized medians average the middle two values.

| Configuration | ns/op | B/op | allocs/op |
|---|---:|---:|---:|
| n=50 keyword | 3,315,327.5 [3,282,102, 3,386,330] | 41,313 [41,312, 41,329] | 819 [819, 819] |
| n=50 synthetic-hybrid | 3,672,819 [3,645,989, 3,731,887] | 149,108.5 [149,106, 149,123] | 2,979 [2,979, 2,979] |
| n=500 keyword | 94,939,562 [94,089,116, 95,697,941] | 41,312 [41,312, 41,312] | 819 [819, 819] |
| n=500 synthetic-hybrid | 98,213,979.5 [97,013,966, 99,640,734] | 869,721 [869,713, 869,754] | 21,684 [21,684, 21,684] |

The fixture uses manually assigned vectors, a tiny labeled dataset and isolated temporary stores; benchmark corpus generation and setup are outside the timed loop. It does not measure real embedding models, network latency, production-scale distributions, live databases or end-to-end agent answers. Hybrid mode is not asserted inside the timing loop. Small sample and host noise limit comparisons.

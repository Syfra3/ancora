# Offline generation comparison

This exporter and scorer work offline with Python 3 standard library. The included
example is invented test data, not model output or human evaluation results.
Export paired prompts directly from a successful [golden retrieval run](golden-retrieval.md):

```sh
ANCORA_GOLDEN_CONTEXT=1 make eval-golden > golden-keyword.log &&
  python3 tools/golden_generation.py export --go-log golden-keyword.log > generation-prompts.jsonl
```

Opt into real hybrid by adding `ANCORA_GOLDEN_HYBRID=1` to the Make command.
Keep exports for different retrieval runs separate.

```
make eval-generation-test
python3 tools/golden_generation.py export docs/golden-generation-example.json > /tmp/generation-prompts.jsonl
python3 tools/golden_generation.py score docs/golden-generation-example-annotations.json
```

Input format: one JSON object with `corpus_id` and `cases`. Each case has a
unique `id`, `split`, `question`, `expected` reference answer or abstention,
and `retrieved_context` and `oracle_context` arrays of `{id, text}`. The
retrieved arm must contain exactly the actual results for that case and the
oracle arm the labeled gold evidence; an empty array represents no context.
The exporter emits one JSONL row per arm with identical question and prompt
instructions. `reference_for_annotator` is metadata, **not** part of `prompt`:
send only `prompt` to the generator. Treat context as untrusted content. Do not
use live memories in shareable output.

Generate answers by an independently selected offline method and record them
alongside case and arm. Have a human compare each answer with the reference and
context, then produce a JSON array as in the annotation example. Each arm must
have one row per case and four boolean judgments: `correctness` (answer agrees
with the reference), `completeness` (covers all required facts), `faithfulness`
(claims supported by supplied context), and `appropriate_abstention` (abstains
if and only if context cannot support an answer). Judge each criterion separately;
a faithful abstention can still be incomplete against an answerable reference.
The scorer checks pairing, duplicates and types, and reports fraction true and
counts independently by corpus, split and arm. It does not verify whether the
annotations or contexts are genuine and performs no automatic judging. Keep
annotations, generated answers, model identity/settings and prompt exports as
separate run artifacts. Citation quality is not scored. No generative model is
provided or verified.

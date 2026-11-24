#!/usr/bin/env python3
"""Export paired generation prompts or summarize human annotations, offline."""
import argparse
import json
import sys
from collections import defaultdict

ARMS = ("retrieved", "oracle")
METRICS = ("correctness", "completeness", "faithfulness", "appropriate_abstention")


def nonempty(value, name):
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"{name} must be a nonempty string")
    return value


def export(data):
    corpus = nonempty(data["corpus_id"], "corpus_id")
    cases = data["cases"]
    if not isinstance(cases, list) or not cases:
        raise ValueError("cases must be a nonempty list")
    seen = set()
    result = []
    for case in cases:
        case_id = nonempty(case["id"], "id")
        if case_id in seen:
            raise ValueError(f"duplicate case: {case_id}")
        seen.add(case_id)
        split = nonempty(case["split"], "split")
        question = nonempty(case["question"], "question")
        # Reference is used for the human rubric, never injected into prompts.
        reference = nonempty(case["expected"], "expected")
        for arm in ARMS:
            context = case[arm + "_context"]
            if not isinstance(context, list):
                raise ValueError(f"{arm}_context must be a list")
            ids = set()
            for item in context:
                ident = nonempty(item["id"], "context id")
                nonempty(item["text"], "context text")
                if ident in ids:
                    raise ValueError(f"duplicate context id: {ident}")
                ids.add(ident)
            rendered = "\n\n".join(f"[{item['id']}] {item['text']}" for item in context) or "(no context)"
            prompt = ("Answer using only the context below. If it does not support an answer, "
                      "say you do not know.\n\nContext:\n" + rendered + "\n\nQuestion: " + question + "\nAnswer:")
            result.append(dict(corpus_id=corpus, case_id=case_id, split=split, arm=arm,
                               context=context, prompt=prompt, reference_for_annotator=reference))
    return result


def score(rows):
    if not isinstance(rows, list) or not rows:
        raise ValueError("annotations must be a nonempty list")
    seen = set()
    paired = defaultdict(set)
    totals = defaultdict(lambda: defaultdict(list))
    for row in rows:
        corpus = nonempty(row["corpus_id"], "corpus_id")
        case = nonempty(row["case_id"], "case_id")
        split = nonempty(row["split"], "split")
        arm = row["arm"]
        if arm not in ARMS:
            raise ValueError(f"invalid arm: {arm}")
        key = (corpus, case, arm)
        if key in seen:
            raise ValueError(f"duplicate annotation: {key}")
        seen.add(key)
        paired[(corpus, case, split)].add(arm)
        for metric in METRICS:
            value = row[metric]
            if type(value) is not bool:
                raise ValueError(f"{metric} must be true or false")
            totals[(corpus, split, arm)][metric].append(value)
    if any(arms != set(ARMS) for arms in paired.values()):
        raise ValueError("each corpus/case/split must have both arms")
    return [dict(corpus_id=corpus, split=split, arm=arm, count=len(values[METRICS[0]]),
                 rates={m: sum(values[m]) / len(values[m]) for m in METRICS})
            for (corpus, split, arm), values in sorted(totals.items())]


def load_go_log(stream):
    rows = []
    corpus = None
    passed = False
    for line in stream:
        if line.startswith("--- PASS: TestGoldenRetrieval ("):
            passed = True
        if not line.startswith("CONTEXT "):
            continue
        row = json.loads(line.removeprefix("CONTEXT "))
        identity = nonempty(row.pop("corpus_id"), "corpus_id")
        if corpus is not None and corpus != identity:
            raise ValueError("mixed corpora in Go log")
        corpus = identity
        rows.append(row)
    if not passed or not rows:
        raise ValueError("expected a successful golden retrieval log with CONTEXT rows")
    return dict(corpus_id=corpus, cases=rows)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("action", choices=("export", "score"))
    parser.add_argument("input", help="JSON input file or Go log; output is written to stdout")
    parser.add_argument("--go-log", action="store_true", help="export from a successful eval-golden log with context enabled")
    args = parser.parse_args()
    if args.go_log and args.action != "export":
        parser.error("--go-log is only valid with export")
    try:
        with open(args.input, encoding="utf-8") as f:
            data = load_go_log(f) if args.go_log else json.load(f)
        rows = export(data) if args.action == "export" else score(data)
        for row in rows:
            print(json.dumps(row, ensure_ascii=False))
    except (ValueError, KeyError, TypeError, OSError) as exc:
        print(f"error: {exc}", file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

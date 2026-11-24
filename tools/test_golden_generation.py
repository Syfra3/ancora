import json
import unittest

from golden_generation import export, load_go_log, score


class GenerationWorkflowTest(unittest.TestCase):
    def test_paired_prompts_and_reference_isolation(self):
        data = {"corpus_id": "toy-v1", "cases": [{"id": "q1", "split": "dev",
                "question": "What color?", "expected": "blue",
                "retrieved_context": [], "oracle_context": [{"id": "e1", "text": "The color is blue."}]}]}
        rows = export(data)
        self.assertEqual([r["arm"] for r in rows], ["retrieved", "oracle"])
        self.assertIn("(no context)", rows[0]["prompt"])
        self.assertNotIn("blue", rows[0]["prompt"])
        self.assertIn("[e1] The color is blue.", rows[1]["prompt"])
        self.assertEqual(rows[0]["reference_for_annotator"], "blue")
        data["cases"].append(data["cases"][0])
        with self.assertRaises(ValueError):
            export(data)

    def test_go_log_requires_success_and_consistent_corpus(self):
        row = dict(corpus_id="toy-v1", id="q1", split="dev", question="Color?",
                   expected="blue", retrieved_context=[], oracle_context=[])
        context = "CONTEXT " + json.dumps(row)
        passed = "--- PASS: TestGoldenRetrieval (0.01s)"
        self.assertEqual(len(export(load_go_log([context, passed]))), 2)
        with self.assertRaises(ValueError):
            load_go_log([context])
        with self.assertRaises(ValueError):
            load_go_log([context, context.replace("toy-v1", "toy-v2"), passed])

    def test_score_separate_arms_and_reject_invalid(self):
        base = dict(corpus_id="toy-v1", case_id="q1", split="dev", correctness=True,
                    completeness=True, faithfulness=True, appropriate_abstention=True)
        rows = [dict(base, arm="retrieved", correctness=False), dict(base, arm="oracle")]
        result = score(rows)
        self.assertEqual({r["arm"]: r["rates"]["correctness"] for r in result},
                         {"retrieved": 0, "oracle": 1})
        with self.assertRaises(ValueError):
            score(rows[:1])
        with self.assertRaises(ValueError):
            score(rows + rows[:1])
        rows[0]["faithfulness"] = 1
        with self.assertRaises(ValueError):
            score(rows)


if __name__ == "__main__":
    unittest.main()

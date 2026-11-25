import json
import tempfile
import unittest
from dataclasses import replace
from pathlib import Path

from contract import Adapter, Evidence, StepReceipt, classify_claim, load_scenarios


SCENARIOS = Path(__file__).with_name("scenarios.json")


class FixtureAdapter:
    """Simulated result producer; cannot authenticate Cloud or run a provider."""

    def __init__(self, provider):
        self.provider = provider

    def execute(self, scenario):
        receipts = tuple(StepReceipt(step, "passed", "https://example.test/fixture-log") for step in scenario.steps)
        return Evidence(self.provider, scenario.id, "simulated", "a" * 40, "", "https://example.test/fixture-run", "https://example.test/fixture-log", receipts, "pass", "clean", False)


class ScenarioContractTests(unittest.TestCase):
    def setUp(self):
        self.cases = load_scenarios(SCENARIOS)
        self.scenario = self.cases["essentials-database-lifecycle"]
        self.evidence = FixtureAdapter("crossplane").execute(self.scenario)

    def test_same_definition_is_consumable_by_three_simulated_adapters(self):
        for provider in ("terraform", "pulumi", "crossplane"):
            with self.subTest(provider=provider):
                adapter: Adapter = FixtureAdapter(provider)
                evidence = adapter.execute(self.scenario)
                self.assertEqual(classify_claim(self.scenario, evidence), "simulated_claim_requires_verification")

    def test_pass_requires_every_step_in_order_and_clean_inventory(self):
        for evidence in (
            replace(self.evidence, steps=self.evidence.steps[:-1]),
            replace(self.evidence, steps=tuple(reversed(self.evidence.steps))),
            replace(self.evidence, steps=(replace(self.evidence.steps[0], outcome="failed"), *self.evidence.steps[1:])),
            replace(self.evidence, steps=(replace(self.evidence.steps[0], evidence_url="http://example.test/"), *self.evidence.steps[1:])),
            replace(self.evidence, cleanup="unknown"),
            replace(self.evidence, cleanup="residual"),
            replace(self.evidence, direct_cloud_recovery=True),
        ):
            with self.subTest(evidence=evidence):
                self.assertEqual(classify_claim(self.scenario, evidence), "invalid")

    def test_provenance_cannot_be_missing_or_misattributed(self):
        for evidence in (
            replace(self.evidence, source_revision="", artifact_digest=""),
            replace(self.evidence, source_revision="abc123"),
            replace(self.evidence, artifact_digest="sha256:invalid"),
            replace(self.evidence, run_url=""),
            replace(self.evidence, evidence_url=""),
            replace(self.evidence, run_url="https://user:password@example.test/run"),
            replace(self.evidence, provider="other"),
            replace(self.evidence, scenario_id="essentials-database-eviction-update"),
            replace(self.evidence, mode="unknown"),
        ):
            with self.subTest(evidence=evidence):
                self.assertEqual(classify_claim(self.scenario, evidence), "invalid")

    def test_live_is_only_an_unverified_claim_and_never_simulated(self):
        steps = tuple(replace(receipt, evidence_url="https://github.com/example/evidence") for receipt in self.evidence.steps)
        live = replace(self.evidence, mode="live", source_revision="", artifact_digest="sha256:" + "a" * 64, run_url="https://github.com/example/run", evidence_url="https://github.com/example/evidence", steps=steps)
        self.assertEqual(classify_claim(self.scenario, live), "live_claim_requires_verification")
        self.assertNotEqual(classify_claim(self.scenario, self.evidence), classify_claim(self.scenario, live))

    def test_live_claim_rejects_fixture_hosts_and_placeholder_identity(self):
        steps = tuple(replace(receipt, evidence_url="https://github.com/example/evidence") for receipt in self.evidence.steps)
        live = replace(self.evidence, mode="live", run_url="https://github.com/example/run", evidence_url="https://github.com/example/evidence", steps=steps)
        for claim in (
            replace(live, run_url="https://fixture.invalid/run"),
            replace(live, run_url="https://example.test/run"),
            replace(live, run_url="https://localhost/run"),
            replace(live, evidence_url="https://FIXTURE.INVALID./log"),
            replace(live, steps=(replace(live.steps[0], evidence_url="https://sub.fixture.invalid/log"), *live.steps[1:])),
            replace(live, source_revision="0" * 40),
            replace(live, artifact_digest="sha256:" + "0" * 64),
            replace(live, source_revision="", artifact_digest="sha256:" + "0" * 64),
        ):
            with self.subTest(claim=claim):
                self.assertEqual(classify_claim(self.scenario, claim), "invalid")

    def test_simulated_fixture_placeholders_remain_simulated(self):
        receipt = replace(self.evidence.steps[0], evidence_url="https://fixture.invalid/log")
        simulated = replace(self.evidence, source_revision="0" * 40, run_url="https://fixture.invalid/run", steps=(receipt, *self.evidence.steps[1:]))
        self.assertEqual(classify_claim(self.scenario, simulated), "simulated_claim_requires_verification")

    def test_unsupported_and_failed_are_not_passes_even_with_residue(self):
        for outcome in ("unsupported", "blocked", "fail"):
            claim = replace(self.evidence, outcome=outcome, cleanup="unknown", steps=())
            self.assertEqual(classify_claim(self.scenario, claim), outcome)

    def test_update_is_distinct_and_cannot_reuse_lifecycle_result(self):
        update = self.cases["essentials-database-eviction-update"]
        self.assertEqual(classify_claim(update, self.evidence), "invalid")
        copied = replace(self.evidence, scenario_id=update.id)
        self.assertEqual(classify_claim(update, copied), "invalid")

    def test_invalid_scenario_documents_fail_closed(self):
        with tempfile.TemporaryDirectory() as temp:
            path = Path(temp) / "scenarios.json"
            for document in (
                {"version": 2, "scenarios": []},
                {"version": True, "scenarios": []},
                {"version": 1, "scenarios": [{"id": "x", "steps": ["create"]}]},
                {"version": 1, "scenarios": [{"id": "x", "steps": ["admission_and_clean_baseline", "admission_and_clean_baseline", "complete_clean_inventory"]}]},
                {"version": 1, "scenarios": [{"id": "x", "steps": ["admission_and_clean_baseline", "complete_clean_inventory"]}] * 2},
            ):
                path.write_text(json.dumps(document), encoding="utf-8")
                with self.subTest(document=document), self.assertRaises(ValueError):
                    load_scenarios(path)


if __name__ == "__main__":
    unittest.main()

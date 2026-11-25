import os
import unittest
from dataclasses import replace
from pathlib import Path
from types import SimpleNamespace
from unittest.mock import patch

from contract import classify_claim, load_scenarios
from crossplane_plan_adapter import CrossplanePlanAdapter


class CrossplanePlanAdapterTests(unittest.TestCase):
    def setUp(self):
        self.cases = load_scenarios(Path(__file__).with_name("scenarios.json"))
        self.case = self.cases["essentials-database-eviction-update"]

    @patch("crossplane_plan_adapter.subprocess.run")
    def test_successful_real_fixture_stays_blocked_without_full_lifecycle(self, run):
        run.return_value = SimpleNamespace(returncode=0, stdout="\n".join((
            "SIMULATED: authenticated fake baseline inventory empty",
            "SIMULATED: managed fake parent created and observed",
            "SIMULATED: managed fake child and parent both read active",
            "SIMULATED: managed fake database update read back with stable ID",
            "SIMULATED: managed fake child 404 while fake parent 200",
            "SIMULATED: managed fake parent 404 and fake inventory empty",
            "--- PASS: TestEssentialsManagedCreateLoopback (1.00s)",
            "--- PASS: TestEssentialsRefreshAndPlan (1.00s)",
            "--- PASS: TestNativeWorkspaceStoreRegenerates (1.00s)",
        )))
        with patch.dict(os.environ, {"QA_TEST_TERRAFORM": "fixture", "QA_TEST_PROVIDER_DIR": "fixture"}):
            result = CrossplanePlanAdapter().execute(self.case)
        self.assertEqual(classify_claim(self.case, result), "blocked")
        self.assertEqual(result.mode, "simulated")
        self.assertEqual(result.steps[0].outcome, "blocked")
        self.assertTrue(all(step.outcome == "passed" for step in result.steps[1:]))
        run.assert_called_once()

    @patch("crossplane_plan_adapter.subprocess.run")
    def test_skipped_or_failed_fixture_cannot_look_successful(self, run):
        with patch.dict(os.environ, {"QA_TEST_TERRAFORM": "fixture", "QA_TEST_PROVIDER_DIR": "fixture"}):
            for output in (SimpleNamespace(returncode=0, stdout="--- SKIP: TestEssentialsRefreshAndPlan"), SimpleNamespace(returncode=0, stdout="--- PASS: TestEssentialsManagedCreateLoopback (1s)\n--- PASS: TestEssentialsRefreshAndPlan (1s)"), SimpleNamespace(returncode=1, stdout="")):
                run.return_value = output
                self.assertEqual(CrossplanePlanAdapter().execute(self.case).outcome, "fail")

    @patch("crossplane_plan_adapter.subprocess.run")
    def test_missing_fixture_or_different_case_never_executes(self, run):
        with patch.dict(os.environ, {}, clear=True):
            self.assertEqual(CrossplanePlanAdapter().execute(self.case).outcome, "blocked")
            self.assertEqual(CrossplanePlanAdapter().execute(self.cases["essentials-database-lifecycle"]).outcome, "unsupported")
            changed = replace(self.case, steps=self.case.steps + ("unverified_extra",))
            self.assertEqual(CrossplanePlanAdapter().execute(changed).outcome, "unsupported")
        run.assert_not_called()

    @patch("crossplane_plan_adapter.subprocess.run")
    def test_changed_shared_definition_cannot_invent_receipts(self, run):
        changed = replace(self.case, steps=self.case.steps + ("unverified_extra",))
        with patch("crossplane_plan_adapter.load_scenarios", return_value={changed.id: changed}):
            self.assertEqual(CrossplanePlanAdapter().execute(changed).outcome, "unsupported")
        run.assert_not_called()


if __name__ == "__main__":
    unittest.main()

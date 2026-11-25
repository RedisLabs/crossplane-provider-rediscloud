"""Run the pinned hosted loopback fixture; never claim a completed lifecycle."""
import os
from pathlib import Path
import subprocess

from contract import Evidence, StepReceipt, load_scenarios

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = "https://fixture.invalid/crossplane-plan"
SUPPORTED_STEPS = (
    "admission_and_clean_baseline", "create_subscription", "read_subscription_active",
    "create_database", "read_database_active", "update_eviction",
    "read_updated_eviction_same_identity", "delete_database",
    "read_database_absent_parent_present", "delete_subscription",
    "read_subscription_absent", "complete_clean_inventory",
)


class CrossplanePlanAdapter:
    def execute(self, scenario):
        expected = load_scenarios(ROOT / "acceptance" / "scenarios.json")["essentials-database-eviction-update"]
        if scenario != expected or expected.steps != SUPPORTED_STEPS:
            return self._result(scenario, "unsupported")
        if not os.environ.get("QA_TEST_TERRAFORM") or not os.environ.get("QA_TEST_PROVIDER_DIR"):
            return self._result(scenario, "blocked")
        try:
            run = subprocess.run(
                ["go", "test", "./scripts/qa_tfplan", "-run", "^TestEssentials(RefreshAndPlan|ManagedCreateLoopback)$", "-count=1", "-v"],
                cwd=ROOT, text=True, capture_output=True, timeout=600, check=False,
            )
        except (OSError, subprocess.TimeoutExpired):
            return self._result(scenario, "fail")
        markers = (
            "SIMULATED: authenticated fake baseline inventory empty",
            "SIMULATED: managed fake parent created and observed",
            "SIMULATED: managed fake child and parent both read active",
            "SIMULATED: managed fake database update read back with stable ID",
            "SIMULATED: managed fake child 404 while fake parent 200",
            "SIMULATED: managed fake parent 404 and fake inventory empty",
            "--- PASS: TestEssentialsManagedCreateLoopback ",
        )
        position = 0
        for marker in markers:
            position = run.stdout.find(marker, position)
            if position < 0:
                return self._result(scenario, "fail")
            position += len(marker)
        if run.returncode != 0 or "--- PASS: TestEssentialsRefreshAndPlan " not in run.stdout:
            return self._result(scenario, "fail")
        # The first receipt remains blocked: a fake empty baseline is not
        # account-wide admission. All other evidence is explicitly simulated.
        receipts = tuple(StepReceipt(step, "blocked" if index == 0 else "passed", FIXTURE) for index, step in enumerate(scenario.steps))
        return self._result(scenario, "blocked", receipts, "clean")

    @staticmethod
    def _result(scenario, outcome, steps=(), cleanup="not_started"):
        return Evidence("crossplane", scenario.id, "simulated", "0" * 40, "", FIXTURE, FIXTURE, steps, outcome, cleanup, False)


if __name__ == "__main__":
    if not os.environ.get("QA_TEST_TERRAFORM") or not os.environ.get("QA_TEST_PROVIDER_DIR"):
        raise SystemExit("pinned fixture dependencies missing")
    case = load_scenarios(ROOT / "acceptance" / "scenarios.json")["essentials-database-eviction-update"]
    claim = CrossplanePlanAdapter().execute(case)
    if claim.outcome != "blocked":
        raise SystemExit(1)
    for receipt in claim.steps:
        print("SIMULATED", receipt.step, receipt.outcome)
    print("BLOCKED shared lifecycle: real account admission is not exercised")

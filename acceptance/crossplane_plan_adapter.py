"""Run the pinned hosted loopback fixture; never claim a completed lifecycle."""
import os
from pathlib import Path
import subprocess

from contract import Evidence, load_scenarios

ROOT = Path(__file__).resolve().parents[1]
FIXTURE = "https://fixture.invalid/crossplane-plan"


class CrossplanePlanAdapter:
    def execute(self, scenario):
        expected = load_scenarios(ROOT / "acceptance" / "scenarios.json")["essentials-database-eviction-update"]
        if scenario != expected:
            return self._result(scenario, "unsupported")
        if not os.environ.get("QA_TEST_TERRAFORM") or not os.environ.get("QA_TEST_PROVIDER_DIR"):
            return self._result(scenario, "blocked")
        try:
            run = subprocess.run(
                ["go", "test", "./scripts/qa_tfplan", "-run", "^TestEssentialsRefreshAndPlan$", "-count=1", "-v"],
                cwd=ROOT, text=True, capture_output=True, timeout=300, check=False,
            )
        except (OSError, subprocess.TimeoutExpired):
            return self._result(scenario, "fail")
        if run.returncode != 0 or "--- PASS: TestEssentialsRefreshAndPlan " not in run.stdout:
            return self._result(scenario, "fail")
        # The fixture exercises real managed reconciliation and pinned Terraform
        # on a fake API, but has neither account admission nor complete lifecycle.
        return self._result(scenario, "blocked")

    @staticmethod
    def _result(scenario, outcome):
        return Evidence("crossplane", scenario.id, "simulated", "0" * 40, "", FIXTURE, FIXTURE, (), outcome, "not_started", False)


if __name__ == "__main__":
    if not os.environ.get("QA_TEST_TERRAFORM") or not os.environ.get("QA_TEST_PROVIDER_DIR"):
        raise SystemExit("pinned fixture dependencies missing")
    case = load_scenarios(ROOT / "acceptance" / "scenarios.json")["essentials-database-eviction-update"]
    claim = CrossplanePlanAdapter().execute(case)
    print("loopback fixture outcome:", claim.outcome, "(no live lifecycle qualification)")
    if claim.outcome != "blocked":
        raise SystemExit(1)

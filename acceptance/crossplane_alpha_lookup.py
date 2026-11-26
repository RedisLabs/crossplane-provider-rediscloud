"""Simulated read-only payment lookup using the real Crossplane reconciler.

The Go test uses an in-memory Kubernetes client and an httptest loopback API.
Its handler rejects non-GET requests and verifies the observed result. It cannot
provide authenticated live Cloud evidence or authorize any live operation.
"""
from __future__ import annotations

import subprocess
from pathlib import Path

from contract import Evidence, StepReceipt, classify_claim, load_scenarios
from go_test_receipts import run_selected

ROOT = Path(__file__).resolve().parent.parent
CASE = "read-only-payment-method-lookup"
EXPECTED = ("read_only_preflight", "lookup_by_identity", "verify_expected_response", "verify_no_cloud_mutations")


def execute(source_revision: str) -> Evidence:
    actual = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    dirty = subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=all"], cwd=ROOT, text=True)
    if actual != source_revision or dirty:
        raise ValueError("source revision does not match a clean checkout")
    scenario = load_scenarios(ROOT / "acceptance" / "alpha-scenarios.json")[CASE]
    if scenario.access != "read_only" or scenario.steps != EXPECTED:
        raise ValueError("unexpected lookup contract")
    run_selected("./internal/controller/lookup", ("TestPaymentMethodReconcileWritesID",), offline=True, timeout=180)
    receipts = tuple(StepReceipt(step, "passed", "https://fixture.invalid/lookup-log") for step in scenario.steps)
    evidence = Evidence("crossplane", CASE, "simulated", source_revision, "", "https://fixture.invalid/lookup-run", "https://fixture.invalid/lookup-log", receipts, "pass", "not_started", False)
    if classify_claim(scenario, evidence) != "simulated_claim_requires_verification":
        raise ValueError("invalid simulated claim")
    return evidence


if __name__ == "__main__":
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    result = execute(revision)
    print(f"{result.mode.upper()} {result.scenario_id}: unverified fixture claim; GET-only loopback")

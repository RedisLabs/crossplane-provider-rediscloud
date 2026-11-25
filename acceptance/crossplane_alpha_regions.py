"""Simulated region lookups through the actual Crossplane reconciler.

The Go fixture uses an in-memory Kubernetes client and a GET-only loopback API.
It is not evidence of a live Cloud lookup or account access.
"""
from __future__ import annotations

import subprocess
from pathlib import Path

from contract import Evidence, StepReceipt, classify_claim, load_scenarios
from go_test_receipts import run_selected

ROOT = Path(__file__).resolve().parent.parent
CASE = "read-only-region-lookup"
EXPECTED = ("read_only_preflight", "lookup_by_identity", "verify_expected_response", "verify_no_cloud_mutations")


def execute(source_revision: str) -> Evidence:
    actual = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    dirty = subprocess.check_output(["git", "status", "--porcelain", "--untracked-files=all"], cwd=ROOT, text=True)
    if actual != source_revision or dirty:
        raise ValueError("source revision does not match a clean checkout")
    scenario = load_scenarios(ROOT / "acceptance" / "alpha-scenarios.json")[CASE]
    if scenario.access != "read_only" or scenario.steps != EXPECTED:
        raise ValueError("unexpected lookup contract")
    run_selected("./internal/controller/lookup", ("TestAlphaRegionLookup", "TestAlphaRegionLookup/AWS", "TestAlphaRegionLookup/GCP"), offline=True, timeout=480)
    receipts = tuple(StepReceipt(step, "passed", "https://fixture.invalid/region-log") for step in scenario.steps)
    evidence = Evidence("crossplane", CASE, "simulated", source_revision, "", "https://fixture.invalid/region-run", "https://fixture.invalid/region-log", receipts, "pass", "not_started", False)
    if classify_claim(scenario, evidence) != "simulated_claim_requires_verification":
        raise ValueError("invalid simulated claim")
    return evidence


if __name__ == "__main__":
    revision = subprocess.check_output(["git", "rev-parse", "HEAD"], cwd=ROOT, text=True).strip()
    result = execute(revision)
    print(f"{result.mode.upper()} {result.scenario_id}: unverified fixture claim; GET-only loopback")

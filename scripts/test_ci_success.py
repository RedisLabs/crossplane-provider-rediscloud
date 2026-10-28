#!/usr/bin/env python3
"""The ci-success job fails on failure or cancellation and accepts skipped jobs."""

from __future__ import annotations

import json
import os
import subprocess
import unittest
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parents[1]
REQUIRED = {
    "detect-noop",
    "lint",
    "check-diff",
    "unit-tests",
    "local-deploy",
    "check-examples",
    "report-breaking-changes",
    "publish-artifacts",
}


def workflow_script() -> str:
    doc = yaml.safe_load((REPO / ".github/workflows/ci.yml").read_text())
    script = doc["jobs"]["ci-success"]["steps"][0]["run"]
    start = script.index("import json")
    code = script[start:].rstrip()
    if code.endswith("PY"):
        code = code[: -len("PY")].rstrip()
    return code


def run_script(needs: dict) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env["NEEDS"] = json.dumps(needs)
    return subprocess.run(
        ["python3", "-c", workflow_script()],
        env=env,
        text=True,
        capture_output=True,
        check=False,
    )


class CiSuccessTest(unittest.TestCase):
    def test_failure_and_cancelled_fail_and_skipped_passes(self):
        docs_only = {name: {"result": "skipped"} for name in REQUIRED}
        docs_only["detect-noop"] = {"result": "success"}
        ok = run_script(docs_only)
        self.assertEqual(ok.returncode, 0, ok.stdout + ok.stderr)
        skipped_gate = run_script({name: {"result": "skipped"} for name in REQUIRED})
        self.assertNotEqual(skipped_gate.returncode, 0)
        self.assertIn("detect-noop=skipped", skipped_gate.stdout)
        broken_gate = {name: {"result": "skipped"} for name in REQUIRED}
        broken_gate["detect-noop"] = {"result": "failure"}
        gate = run_script(broken_gate)
        self.assertNotEqual(gate.returncode, 0)
        self.assertIn("detect-noop=failure", gate.stdout)
        failed = run_script({"unit-tests": {"result": "failure"}, "lint": {"result": "skipped"}, "detect-noop": {"result": "success"}})
        self.assertNotEqual(failed.returncode, 0)
        self.assertIn("unit-tests=failure", failed.stdout)
        cancelled = run_script({"local-deploy": {"result": "cancelled"}, "detect-noop": {"result": "success"}})
        self.assertNotEqual(cancelled.returncode, 0)
        self.assertIn("local-deploy=cancelled", cancelled.stdout)

    def test_workflow_job_does_not_need_nightly_or_uptest(self):
        doc = yaml.safe_load((REPO / ".github/workflows/ci.yml").read_text())
        job = doc["jobs"]["ci-success"]
        self.assertEqual(set(job["needs"]), REQUIRED)
        self.assertNotIn("qa-live", job["needs"])
        self.assertNotIn("Uptest", "\n".join(job["needs"]))


if __name__ == "__main__":
    unittest.main()

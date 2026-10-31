#!/usr/bin/env python3
"""Guards for alignment gaps that are already fixed."""

from __future__ import annotations

import shutil
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

import yaml

REPO = Path(__file__).resolve().parents[1]
CHECK = REPO / "scripts" / "check-examples.py"
CRD = REPO / "package" / "crds" / "rediscloud.redis.io_subscriptions.yaml"
EXAMPLE = REPO / "examples" / "rediscloud" / "subscription.yaml"


def run_check(crd_dir: Path, examples_dir: Path) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        [sys.executable, str(CHECK), str(crd_dir), str(examples_dir)],
        check=False,
        capture_output=True,
        text=True,
    )


class CheckExamplesTests(unittest.TestCase):
    def test_unknown_for_provider_field_fails_and_restore_passes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            crd_dir = root / "crds"
            examples_dir = root / "examples"
            crd_dir.mkdir()
            examples_dir.mkdir()
            shutil.copy(CRD, crd_dir / CRD.name)
            example_path = examples_dir / EXAMPLE.name
            shutil.copy(EXAMPLE, example_path)

            passed = run_check(crd_dir, examples_dir)
            self.assertEqual(passed.returncode, 0, passed.stdout + passed.stderr)

            document = yaml.safe_load(example_path.read_text())
            document["spec"]["forProvider"]["notARealField"] = "no"
            example_path.write_text(yaml.safe_dump(document))
            failed = run_check(crd_dir, examples_dir)
            self.assertNotEqual(failed.returncode, 0)
            self.assertIn("not in the CRD schema", failed.stdout + failed.stderr)

            shutil.copy(EXAMPLE, example_path)
            restored = run_check(crd_dir, examples_dir)
            self.assertEqual(restored.returncode, 0, restored.stdout + restored.stderr)


class GenerateCleanupTests(unittest.TestCase):
    def test_generate_keeps_served_versions(self) -> None:
        text = (REPO / "apis" / "generate.go").read_text()
        self.assertIn("-not -path '*/v1alpha1/*'", text)
        self.assertIn("-not -path '*/v1beta1/*'", text)


class GoVersionTests(unittest.TestCase):
    def test_workflows_and_makefile_use_go_1_26(self) -> None:
        ci = yaml.safe_load((REPO / ".github/workflows/ci.yml").read_text())
        uptest = yaml.safe_load((REPO / ".github/workflows/uptest-trigger.yaml").read_text())
        publish = yaml.safe_load((REPO / ".github/workflows/publish-provider-package.yml").read_text())
        makefile = (REPO / "Makefile").read_text()
        trigger = publish[True]["workflow_dispatch"]["inputs"]["go-version"]["default"]

        self.assertEqual(ci["env"]["GO_VERSION"], "1.26")
        self.assertEqual(uptest["env"]["GO_VERSION"], "1.26")
        self.assertEqual(trigger, "1.26")
        self.assertIn("GO_REQUIRED_VERSION ?= 1.26", makefile)


if __name__ == "__main__":
    unittest.main()

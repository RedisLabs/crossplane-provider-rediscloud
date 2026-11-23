#!/usr/bin/env python3
"""Credential-free encryption and interrupted-worker recovery tests."""
import base64
import hashlib
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path

import qa_live
import qa_recovery_resolve
import qa_verify_source

SCRIPT = str(Path(__file__).with_name("qa_checkpoint.mjs"))


class CheckpointTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = str(Path(self.temp.name) / "intent.enc")
        origin = "https://qa.example.invalid"
        self.env = dict(os.environ, QA_CHECKPOINT_KEY=base64.b64encode(bytes(range(32))).decode(),
                        UPTEST_CLOUD_CREDENTIALS=json.dumps({"url": origin + "/v1", "api_key": "test-only"}),
                        QA_VERIFIED_ORIGIN_HASHES=hashlib.sha256(origin.encode()).hexdigest(),
                        GITHUB_REPOSITORY="RedisLabs/crossplane-provider-rediscloud", GITHUB_RUN_ID="12345",
                        GITHUB_RUN_ATTEMPT="2", GITHUB_SHA="a" * 40)

    def run_node(self, *args, env=None):
        return subprocess.run(["node", SCRIPT, *args], env=env or self.env, capture_output=True, text=True)

    def test_lost_worker_leaves_encrypted_intent_recoverable_by_another_process(self):
        # Fake immutable artifact storage outlives the worker's local directory.
        remote_path = str(Path(self.temp.name) / "remote-artifact.enc")
        worker = subprocess.run(["python3", "-c",
            "import os,shutil,subprocess,sys; subprocess.run(['node',sys.argv[1],'create',sys.argv[2]],check=True); shutil.copyfile(sys.argv[2],sys.argv[3]); os._exit(97)",
            SCRIPT, self.path, remote_path], env=self.env, capture_output=True)
        self.assertEqual(worker.returncode, 97)
        Path(self.path).unlink()
        ciphertext = Path(remote_path).read_text()
        self.assertNotIn("xp-ci-12345-2", ciphertext)
        result = self.run_node("resolve", remote_path, "12345", "2")
        self.assertEqual(result.returncode, 0, result.stderr)
        intent = json.loads(Path(remote_path + ".resolved").read_text())
        self.assertEqual(intent["runName"], "xp-ci-12345-2")
        self.assertEqual(intent["commit"], "a" * 40)

    def test_wrong_run_wrong_key_and_tampering_fail_closed(self):
        self.assertEqual(self.run_node("create", self.path).returncode, 0)
        self.assertNotEqual(self.run_node("resolve", self.path, "999", "2").returncode, 0)
        bad_env = dict(self.env, QA_CHECKPOINT_KEY=base64.b64encode(b"x" * 32).decode())
        self.assertNotEqual(self.run_node("resolve", self.path, "12345", "2", env=bad_env).returncode, 0)
        data = json.loads(Path(self.path).read_text())
        data["tag"] = base64.b64encode(b"x" * 16).decode()
        Path(self.path).write_text(json.dumps(data))
        self.assertNotEqual(self.run_node("resolve", self.path, "12345", "2").returncode, 0)
        self.assertFalse(Path(self.path + ".resolved").exists())

    def test_unverified_endpoint_cannot_create_intent(self):
        bad = dict(self.env, QA_VERIFIED_ORIGIN_HASHES="0" * 64)
        self.assertNotEqual(self.run_node("create", self.path, env=bad).returncode, 0)
        self.assertFalse(Path(self.path).exists())


class ResolutionTest(unittest.TestCase):
    def setUp(self):
        self.intent = {"runName": "xp-ci-12345-2"}
        self.logs = []

    def test_invisible_subscription_is_uncertain_not_clean(self):
        class Cloud:
            def list_fixed_subscriptions_checked(self): return []
        self.assertEqual(qa_recovery_resolve.resolve(Cloud(), self.intent, self.logs.append), "uncertain")
        self.assertIn("asynchronous creation remains possible", self.logs[0])

    def test_exact_ids_and_foreign_inventory_are_read_without_mutation(self):
        class Cloud:
            def list_fixed_subscriptions_checked(self): return [{"id": 9, "name": "xp-ci-12345-2"}]
            def get_fixed_subscription(self, sub): return 200, {"id": 9, "name": "xp-ci-12345-2"}
            def list_fixed_databases_complete(self, sub):
                return 200, [{"databaseId": 11, "name": "xp-ci-12345-2-db"}, {"databaseId": 12, "name": "foreign"}]
            def get_fixed_database(self, sub, db): return 200, {"databaseId": 11, "name": "xp-ci-12345-2-db"}
        self.assertEqual(qa_recovery_resolve.resolve(Cloud(), self.intent, self.logs.append), "present")
        self.assertIn("foreign databases=1", self.logs[0])
        self.assertNotIn("12345", self.logs[0])

    def test_duplicate_name_and_mismatched_parent_fail_closed(self):
        class Cloud:
            def list_fixed_subscriptions_checked(self): return [{"id": 9, "name": "xp-ci-12345-2"}, {"id": 10, "name": "xp-ci-12345-2"}]
        with self.assertRaises(qa_live.SuiteFailure):
            qa_recovery_resolve.resolve(Cloud(), self.intent, self.logs.append)

    def test_source_workflow_must_be_completed_and_match_identity(self):
        intent = {"repo": "RedisLabs/crossplane-provider-rediscloud", "runId": "12345",
                  "attempt": "2", "commit": "a" * 40}
        source = {"id": 12345, "run_attempt": 2, "head_sha": "a" * 40,
                  "path": ".github/workflows/qa-live.yaml", "event": "workflow_dispatch", "status": "completed"}
        qa_verify_source.verify(intent, source)
        for field, value in (("status", "in_progress"), ("id", 999), ("run_attempt", 1),
                             ("head_sha", "b" * 40), ("event", "schedule"), ("path", "other")):
            bad = dict(source, **{field: value})
            with self.assertRaises(ValueError):
                qa_verify_source.verify(intent, bad)


if __name__ == "__main__":
    unittest.main()

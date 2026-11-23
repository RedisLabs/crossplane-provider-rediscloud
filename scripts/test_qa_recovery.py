#!/usr/bin/env python3
"""Credential-free encryption and interrupted-worker recovery tests."""
import base64
import hashlib
import json
import os
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch
from datetime import datetime, timezone
from pathlib import Path

import qa_live
import qa_capture_task_baseline
import qa_recovery_resolve
import qa_verify_source

SCRIPT = str(Path(__file__).with_name("qa_checkpoint.mjs"))
BASELINE = json.dumps({"capturedAt": "2026-11-23T23:00:00Z", "taskIds": ["00000000-0000-0000-0000-000000000001"]})


class CheckpointTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = str(Path(self.temp.name) / "intent.enc")
        origin = "https://qa.example.invalid"
        self.env = dict(os.environ, QA_CHECKPOINT_KEY=base64.b64encode(bytes(range(32))).decode(),
                        UPTEST_CLOUD_CREDENTIALS=json.dumps({"url": origin + "/v1", "api_key": "test-only", "secret_key": "test-secret"}),
                        QA_VERIFIED_ORIGIN_HASHES=hashlib.sha256(origin.encode()).hexdigest(),
                        GITHUB_REPOSITORY="RedisLabs/crossplane-provider-rediscloud", GITHUB_RUN_ID="12345",
                        GITHUB_RUN_ATTEMPT="2", GITHUB_SHA="a" * 40, QA_CHECKPOINT_KIND="strict")

    def run_node(self, *args, env=None, data=BASELINE):
        return subprocess.run(["node", SCRIPT, *args], env=env or self.env, input=data, capture_output=True, text=True)

    def test_lost_worker_leaves_encrypted_intent_recoverable_by_another_process(self):
        # Fake immutable artifact storage outlives the worker's local directory.
        remote_path = str(Path(self.temp.name) / "remote-artifact.enc")
        worker = subprocess.run(["python3", "-c",
            "import os,shutil,subprocess,sys; subprocess.run(['node',sys.argv[1],'create',sys.argv[2]],input=sys.argv[4],text=True,check=True); shutil.copyfile(sys.argv[2],sys.argv[3]); os._exit(97)",
            SCRIPT, self.path, remote_path, BASELINE], env=self.env, capture_output=True)
        self.assertEqual(worker.returncode, 97)
        Path(self.path).unlink()
        ciphertext = Path(remote_path).read_text()
        self.assertNotIn("xp-ci-12345-2", ciphertext)
        result = self.run_node("resolve", remote_path, "12345", "2")
        self.assertEqual(result.returncode, 0, result.stderr)
        intent = json.loads(Path(remote_path + ".resolved").read_text())
        self.assertEqual(intent["runName"], "xp-ci-12345-2")
        self.assertEqual(intent["commit"], "a" * 40)
        self.assertEqual(len(intent["taskBaseline"]["taskIds"]), 1)

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

    def test_invalid_or_missing_checkpoint_kind_fails_closed(self):
        for kind in ("", "bogus"):
            bad = dict(self.env, QA_CHECKPOINT_KIND=kind)
            self.assertNotEqual(self.run_node("create", self.path, env=bad).returncode, 0)
            self.assertFalse(Path(self.path).exists())

    def test_malformed_baseline_input_fails_closed(self):
        invalid = ("", "not-json", json.dumps({"capturedAt": "2026-11-23T23:00:00Z", "taskIds": ["bad"]}),
                   json.dumps({"capturedAt": "2026-11-23T23:00:00Z", "taskIds": ["00000000-0000-0000-0000-000000000001"] * 2}))
        for data in invalid:
            self.assertNotEqual(self.run_node("create", self.path, data=data).returncode, 0)
            self.assertFalse(Path(self.path).exists())

    def test_rehearsal_intent_round_trip_cannot_authorize_real_recovery(self):
        rehearsal = dict(self.env, QA_CHECKPOINT_KIND="rehearsal")
        self.assertEqual(self.run_node("create", self.path, env=rehearsal).returncode, 0)
        self.assertEqual(self.run_node("resolve", self.path, "12345", "2", env=rehearsal).returncode, 0)
        intent = json.loads(Path(self.path + ".resolved").read_text())
        self.assertEqual(intent["kind"], "rehearsal")
        with self.assertRaises(ValueError):
            qa_verify_source.verify(intent, {})

    def test_recovery_entry_authenticates_encrypted_intent_and_source_before_mutation(self):
        self.assertEqual(self.run_node("create", self.path).returncode, 0)
        argv = ["qa_recovery_resolve.py", self.path, "--recover-exact", "--source-run-id", "12345", "--source-attempt", "2"]
        with patch.dict(os.environ, self.env), patch.object(sys, "argv", argv), \
             patch.object(qa_verify_source, "verify_remote", side_effect=ValueError("untrusted")), \
             patch.object(qa_recovery_resolve, "recover_exact") as mutation:
            self.assertEqual(qa_recovery_resolve.main(), 1)
            mutation.assert_not_called()
        with patch.dict(os.environ, self.env), patch.object(sys, "argv", argv), \
             patch.object(qa_verify_source, "verify_remote") as verify, \
             patch.object(qa_recovery_resolve, "recover_exact") as mutation:
            self.assertEqual(qa_recovery_resolve.main(), 0)
            verify.assert_called_once()
            mutation.assert_called_once()
        Path(self.path).write_text('{"runName":"xp-ci-12345-2"}')
        with patch.dict(os.environ, self.env), patch.object(sys, "argv", argv), \
             patch.object(qa_recovery_resolve, "recover_exact") as mutation:
            self.assertEqual(qa_recovery_resolve.main(), 1)
            mutation.assert_not_called()


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
        intent = {"kind": "strict", "repo": "RedisLabs/crossplane-provider-rediscloud", "runId": "12345",
                  "attempt": "2", "commit": "a" * 40}
        source = {"id": 12345, "run_attempt": 2, "head_sha": "a" * 40,
                  "path": ".github/workflows/qa-live.yaml", "event": "workflow_dispatch", "status": "completed"}
        qa_verify_source.verify(intent, source)
        with self.assertRaises(ValueError):
            qa_verify_source.verify(dict(intent, kind="rehearsal"), source)
        for field, value in (("status", "in_progress"), ("id", 999), ("run_attempt", 1),
                             ("head_sha", "b" * 40), ("event", "schedule"), ("path", "other")):
            bad = dict(source, **{field: value})
            with self.assertRaises(ValueError):
                qa_verify_source.verify(intent, bad)

    def test_task_baseline_rejects_ambiguous_or_incomplete_data(self):
        class Cloud:
            def __init__(self, response): self.response = response
            def request(self, method, path):
                if method != "GET" or path != "/tasks": raise AssertionError("unexpected mutation")
                return self.response
        task = {"taskId": "00000000-0000-0000-0000-000000000001"}
        self.assertEqual(len(qa_capture_task_baseline.baseline(Cloud((200, {"tasks": [task]})))["taskIds"]), 1)
        self.assertEqual(len(qa_capture_task_baseline.baseline(Cloud((200, {"tasks": [task, task]})))["taskIds"]), 1)
        conflict = dict(task, status="different")
        for response in ((403, None), (200, {}), (200, {"tasks": [task], "next": "more"}),
                         (200, {"tasks": [task], "totalCount": 2}),
                         (200, {"tasks": [task, conflict]}), (200, {"tasks": [{"taskId": "bad"}]})):
            with self.assertRaises(qa_live.SuiteFailure):
                qa_capture_task_baseline.baseline(Cloud(response))

    def test_task_delta_reports_new_and_missing_without_identifiers(self):
        captured = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ")
        intent = {"taskBaseline": {"capturedAt": captured, "taskIds": ["before", "shared"]}}
        with patch.object(qa_capture_task_baseline, "task_rows", return_value={"shared": {}, "new": {}}):
            qa_recovery_resolve.task_delta(object(), intent, self.logs.append)
            with self.assertRaises(KeyError):
                qa_recovery_resolve.task_delta(object(), {}, self.logs.append)
        self.assertIn("new=1 missing_baseline=1", self.logs[0])
        self.assertNotIn("shared", self.logs[0])

    def test_expired_task_baseline_cannot_support_correlation(self):
        intent = {"taskBaseline": {"capturedAt": "2000-01-01T00:00:00Z", "taskIds": []}}
        with self.assertRaises(qa_live.SuiteFailure):
            qa_recovery_resolve.task_delta(object(), intent, self.logs.append)

    def test_discovery_continues_when_account_task_list_is_unavailable(self):
        class Cloud:
            def list_fixed_subscriptions_checked(self): return []
        with patch.object(qa_capture_task_baseline, "task_rows", side_effect=qa_live.SuiteFailure("no access")):
            self.assertEqual(qa_recovery_resolve.discover(Cloud(), self.intent, self.logs.append), 2)
        self.assertTrue(any("task delta unavailable" in line for line in self.logs))
        self.assertTrue(any("no subscription visible" in line for line in self.logs))

    def test_create_task_correlation_requires_exact_ids_parent_and_active_resources(self):
        tasks = {
            "00000000-0000-0000-0000-000000000001": {"commandType": "fixedSubscriptionCreateRequest"},
            "00000000-0000-0000-0000-000000000002": {"commandType": "fixedDatabaseCreateRequest"},
        }
        details = {"subscriptionId": "9", "databaseId": "11", "subscriptionStatus": "active", "databaseStatus": "active", "foreign": 0}
        class Cloud:
            def request(self, method, path):
                task_id = path.split("/")[-1]
                if task_id.endswith("1"):
                    return 200, {"taskId": task_id, "commandType": "fixedSubscriptionCreateRequest", "status": "processing-completed", "response": {"resourceId": 9}}
                return 200, {"taskId": task_id, "commandType": "fixedDatabaseCreateRequest", "status": "processing-completed", "response": {"resourceId": 11, "additionalResourceId": 9}}
        cloud = Cloud()
        self.assertTrue(qa_recovery_resolve.task_evidence(cloud, tasks, 0, details, self.logs.append))
        self.assertFalse(qa_recovery_resolve.task_evidence(cloud, tasks, 1, details, self.logs.append))
        self.assertFalse(qa_recovery_resolve.task_evidence(cloud, tasks, 0, dict(details, databaseStatus="pending"), self.logs.append))
        self.assertFalse(qa_recovery_resolve.task_evidence(cloud, tasks, 0, dict(details, subscriptionId="10"), self.logs.append))
        self.assertFalse(qa_recovery_resolve.task_evidence(cloud, tasks, 0, dict(details, foreign=1), self.logs.append))
        extra = dict(tasks, **{"00000000-0000-0000-0000-000000000003": {"commandType": "unknown"}})
        self.assertFalse(qa_recovery_resolve.task_evidence(cloud, extra, 0, details, self.logs.append))
        duplicate = dict(tasks, **{"00000000-0000-0000-0000-000000000003": {"commandType": "fixedDatabaseCreateRequest"}})
        self.assertFalse(qa_recovery_resolve.task_evidence(cloud, duplicate, 0, details, self.logs.append))
        self.assertFalse(qa_recovery_resolve.task_evidence(cloud, {}, 0, details, self.logs.append))
        class BadCloud:
            def __init__(self, code, body): self.code, self.body = code, body
            def request(self, method, path): return self.code, self.body
        self.assertFalse(qa_recovery_resolve.task_evidence(BadCloud(403, None), tasks, 0, details, self.logs.append))
        pending = {"taskId": "00000000-0000-0000-0000-000000000001", "commandType": "fixedSubscriptionCreateRequest", "status": "received", "response": {"resourceId": 9}}
        self.assertFalse(qa_recovery_resolve.task_evidence(BadCloud(200, pending), tasks, 0, details, self.logs.append))
        self.assertFalse(qa_recovery_resolve.task_evidence(BadCloud(200, dict(pending, taskId="different")), tasks, 0, details, self.logs.append))
        self.assertFalse(qa_recovery_resolve.task_evidence(BadCloud(200, dict(pending, commandType="other")), tasks, 0, details, self.logs.append))

    def test_discovery_exit_depends_on_complete_task_and_resource_evidence(self):
        with patch.object(qa_recovery_resolve, "task_delta", return_value=({}, 0)), \
             patch.object(qa_recovery_resolve, "resolve", return_value="present"), \
             patch.object(qa_recovery_resolve, "task_evidence", return_value=True):
            self.assertEqual(qa_recovery_resolve.discover(object(), self.intent, self.logs.append), 0)
        with patch.object(qa_recovery_resolve, "task_delta", return_value=({}, 0)), \
             patch.object(qa_recovery_resolve, "resolve", return_value="present"), \
             patch.object(qa_recovery_resolve, "task_evidence", return_value=False):
            self.assertEqual(qa_recovery_resolve.discover(object(), self.intent, self.logs.append), 2)

    def test_exact_recovery_deletes_child_before_parent_and_preserves_failed_qualification(self):
        cloud, intent, tasks = recovery_fixture()
        with patch.object(qa_capture_task_baseline, "task_rows", return_value=tasks):
            qa_recovery_resolve.recover_exact(cloud, intent, self.logs.append, timeout=0)
        self.assertEqual(cloud.deletes, ["database", "subscription"])
        self.assertTrue(any("original qualification remains failed" in line for line in self.logs))

    def test_exact_recovery_refuses_prior_delete_or_ambiguous_tasks_without_writes(self):
        cloud, intent, tasks = recovery_fixture()
        tasks["00000000-0000-0000-0000-000000000003"] = {"commandType": "fixedDatabaseDeleteRequest"}
        with patch.object(qa_capture_task_baseline, "task_rows", return_value=tasks):
            with self.assertRaises(qa_live.SuiteFailure):
                qa_recovery_resolve.recover_exact(cloud, intent, self.logs.append, timeout=0)
        self.assertEqual(cloud.deletes, [])

    def test_exact_recovery_preserves_parent_when_child_absence_unverified(self):
        cloud, intent, tasks = recovery_fixture()
        cloud.fail_db_read_after_delete = True
        with patch.object(qa_capture_task_baseline, "task_rows", return_value=tasks):
            with self.assertRaises(qa_live.SuiteFailure):
                qa_recovery_resolve.recover_exact(cloud, intent, self.logs.append, timeout=0)
        self.assertEqual(cloud.deletes, ["database"])

    def test_exact_recovery_refuses_foreign_database_and_inactive_resource(self):
        for change in ("foreign", "pending"):
            cloud, intent, tasks = recovery_fixture()
            setattr(cloud, change, True)
            with patch.object(qa_capture_task_baseline, "task_rows", return_value=tasks):
                with self.assertRaises(qa_live.SuiteFailure):
                    qa_recovery_resolve.recover_exact(cloud, intent, self.logs.append, timeout=0)
            self.assertEqual(cloud.deletes, [])

    def test_exact_recovery_polls_delayed_child_and_times_out_safely(self):
        for delay, timeout, expected in ((1, 2, ["database", "subscription"]), (100, 2, ["database"])):
            cloud, intent, tasks = recovery_fixture()
            cloud.db_delay = delay
            clock = [0]
            def sleep(seconds): clock[0] += seconds
            with patch.object(qa_capture_task_baseline, "task_rows", return_value=tasks):
                if delay == 1:
                    qa_recovery_resolve.recover_exact(cloud, intent, self.logs.append, timeout=timeout,
                                                      poll=1, now=lambda: clock[0], sleep=sleep)
                else:
                    with self.assertRaises(qa_live.SuiteFailure):
                        qa_recovery_resolve.recover_exact(cloud, intent, self.logs.append, timeout=timeout,
                                                          poll=1, now=lambda: clock[0], sleep=sleep)
            self.assertEqual(cloud.deletes, expected)
            self.assertGreater(clock[0], 0)

    def test_exact_recovery_rejects_subscription_timeout_and_stale_inventory(self):
        for change in ("sub_stuck", "stale_inventory", "sub_forbidden"):
            cloud, intent, tasks = recovery_fixture()
            setattr(cloud, change, True)
            with patch.object(qa_capture_task_baseline, "task_rows", return_value=tasks):
                with self.assertRaises(qa_live.SuiteFailure):
                    qa_recovery_resolve.recover_exact(cloud, intent, self.logs.append, timeout=0)
            self.assertEqual(cloud.deletes, ["database", "subscription"])


def recovery_fixture():
    subtask = "00000000-0000-0000-0000-000000000001"
    dbtask = "00000000-0000-0000-0000-000000000002"
    tasks = {subtask: {"commandType": "fixedSubscriptionCreateRequest"},
             dbtask: {"commandType": "fixedDatabaseCreateRequest"}}
    class Cloud:
        def __init__(self):
            self.deletes = []
            self.db_gone = False
            self.sub_gone = False
            self.foreign = False
            self.pending = False
            self.fail_db_read_after_delete = False
            self.db_delay = 0
            self.sub_stuck = False
            self.sub_forbidden = False
            self.stale_inventory = False
        def list_fixed_subscriptions_checked(self):
            return [] if self.sub_gone and not self.stale_inventory else [{"id": 9, "name": "xp-ci-12345-2"}]
        def get_fixed_subscription(self, sub):
            if self.sub_gone and self.sub_forbidden: return 403, None
            return (404, None) if self.sub_gone and not self.sub_stuck else (200, {"id": 9, "name": "xp-ci-12345-2", "status": "active"})
        def list_fixed_databases_complete(self, sub):
            rows = [] if self.db_gone else [{"databaseId": 11, "name": "xp-ci-12345-2-db"}]
            return 200, rows + ([{"databaseId": 12, "name": "foreign"}] if self.foreign else [])
        def get_fixed_database(self, sub, db):
            if self.db_gone:
                if self.db_delay:
                    self.db_delay -= 1
                    return 200, {"databaseId": 11, "name": "xp-ci-12345-2-db", "status": "active"}
                return (403, None) if self.fail_db_read_after_delete else (404, None)
            return 200, {"databaseId": 11, "name": "xp-ci-12345-2-db", "status": "pending" if self.pending else "active"}
        def request(self, method, path):
            task_id = path.split("/")[-1]
            if task_id == subtask:
                return 200, {"taskId": task_id, "commandType": "fixedSubscriptionCreateRequest", "status": "processing-completed", "response": {"resourceId": 9}}
            return 200, {"taskId": task_id, "commandType": "fixedDatabaseCreateRequest", "status": "processing-completed", "response": {"resourceId": 11, "additionalResourceId": 9}}
        def delete_fixed_database(self, sub, db):
            self.deletes.append("database")
            self.db_gone = True
        def delete_fixed_subscription(self, sub):
            self.deletes.append("subscription")
            self.sub_gone = True
    intent = {"runName": "xp-ci-12345-2", "taskBaseline": {"capturedAt": datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%SZ"), "taskIds": []}}
    return Cloud(), intent, tasks


if __name__ == "__main__":
    unittest.main()

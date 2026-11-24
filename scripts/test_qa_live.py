#!/usr/bin/env python3
"""Hermetic tests for the QA create-and-delete script."""

from __future__ import annotations

import unittest
import base64
import hashlib
import hmac
import json
import tempfile
import threading
from datetime import datetime, timezone
from types import SimpleNamespace
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from unittest.mock import patch

import qa_live
import qa_capture_task_baseline

REPO = Path(__file__).resolve().parents[1]
PAYMENT_ID = "should-not-print"
PAYMENT_NUMBER = "4242"
PLAN_NUMBER = "7"
API_KEY = "api-key-should-not-print"
SECRET_KEY = "secret-key-should-not-print"


class Clock:
    def __init__(self):
        self.t = 0.0

    def now(self) -> float:
        return self.t

    def sleep(self, seconds: float) -> None:
        self.t += seconds


class World:
    def __init__(self):
        self.applied = []
        self.deleted = []
        self.cloud_deletes = []
        self.subs = []
        self.rules = []
        self.roles = []
        self.users = []
        self.fixed = []
        self.fixed_databases = []
        self.objects = {}
        self.installed = False
        self.delete_clears = True
        self.fail_database = False
        self.fail_essentials = False
        self.secret_keys = ["private_endpoint", "public_endpoint"]
        self.database_status = "active"
        self.subscription_status = "active"
        self.database_http = None
        self.subscription_http = None
        self.subscription_gone = False
        self.database_gone = False
        self.database_applied = False
        self.database_gets = []
        self.fixed_database_gets = []
        self.ess_db_applied = False
        self.ess_sub_gone = False
        self.ess_db_gone = False
        self.eviction = "volatile-lru"

    def install_provider_config(self) -> None:
        self.installed = True

    def record_controller_evidence(self, run_name: str) -> None:
        pass

    def require_deleted_event(self, uid: str, label: str) -> None:
        pass

    def apply(self, manifest: dict) -> None:
        if manifest["kind"] == "Database" and self.fail_database and not str(manifest.get("apiVersion", "")).startswith("essentials."):
            raise qa_live.SuiteFailure("database step failed")
        if manifest["kind"] == "Subscription" and self.fail_essentials and str(manifest.get("apiVersion", "")).startswith("essentials."):
            raise qa_live.SuiteFailure("essentials step failed")
        self.applied.append(manifest)
        name = manifest["metadata"]["name"]
        kind = manifest["kind"]
        api = manifest.get("apiVersion") or ""
        if kind == "Subscription" and api.startswith("essentials."):
            self.fixed.append({"id": "200", "name": name, "status": "active"})
            self.objects[(qa_live.ESS_SUBSCRIPTION, name)] = ready("200")
        elif kind == "Subscription":
            self.subs.append({"id": "100", "name": name, "status": self.subscription_status})
            self.objects[(qa_live.SUBSCRIPTION, name)] = ready("100")
        elif kind == "Rule":
            self.rules.append({"name": name, "status": "active", "acl": qa_live.ACL_RULE})
            self.objects[(qa_live.ACL, name)] = ready("1")
        elif kind == "Role":
            self.roles.append({"name": name, "status": "active"})
            self.objects[(qa_live.ROLE, name)] = ready("3")
        elif kind == "User":
            self.users.append({"name": name, "status": "active"})
            self.objects[(qa_live.USER, name)] = ready("4")
        elif kind == "Database" and api.startswith("essentials."):
            self.ess_db_applied = True
            self.ess_db_name = manifest["spec"]["forProvider"]["name"]
            self.objects[(qa_live.ESS_DATABASE, name)] = ready("200/60")
            self.eviction = manifest["spec"]["forProvider"].get("dataEviction", self.eviction)
            self.objects[(qa_live.ESS_DATABASE, name)]["status"]["atProvider"]["dataEviction"] = self.eviction
        elif kind == "Database":
            self.database_applied = True
            self.database_name = manifest["spec"]["forProvider"]["name"]
            self.objects[(qa_live.DATABASE, name)] = ready("100/50")
        elif kind == "PaymentMethod":
            self.objects[(qa_live.PAYMENT, name)] = ready("1")
        elif kind == "EssentialsPlan":
            self.objects[(qa_live.PLAN, name)] = ready("1")
            self.objects[(qa_live.PLAN, name)]["status"]["atProvider"] = {
                "id": PLAN_NUMBER, "cloudProvider": "GCP", "region": "us-central1"
            }
        elif kind == "Regions":
            self.objects[(qa_live.REGIONS, name)] = ready("1")

    def delete(self, resource: str, name: str, namespace: str | None = None) -> None:
        self.deleted.append((resource, name))
        if resource == qa_live.SUBSCRIPTION:
            self.subscription_gone = True
            if self.delete_clears:
                self.subs = [row for row in self.subs if row.get("name") != name]
        if resource == qa_live.DATABASE:
            self.database_gone = True
        if resource == qa_live.ACL:
            self.rules = [row for row in self.rules if row.get("name") != name]
        if resource == qa_live.ROLE:
            self.roles = [row for row in self.roles if row.get("name") != name]
        if resource == qa_live.USER:
            self.users = [row for row in self.users if row.get("name") != name]
        if resource == qa_live.ESS_SUBSCRIPTION:
            self.ess_sub_gone = True
            if self.delete_clears:
                self.fixed = [row for row in self.fixed if row.get("name") != name]
        if resource == qa_live.ESS_DATABASE:
            self.ess_db_gone = True
        self.objects.pop((resource, name), None)

    def get(self, resource: str, name: str, namespace: str | None = None):
        return self.objects.get((resource, name))

    def secret_key_names(self, namespace: str, name: str) -> list[str]:
        return list(self.secret_keys)

    def payment_method_id(self) -> str:
        return PAYMENT_NUMBER

    def plan_id(self) -> str:
        return PLAN_NUMBER

    def list_subscriptions(self) -> list:
        return [dict(row) for row in self.subs]

    def delete_subscription(self, sub_id: str) -> None:
        self.cloud_deletes.append(sub_id)
        if self.delete_clears:
            self.subs = [row for row in self.subs if str(row.get("id")) != str(sub_id)]

    def get_subscription(self, sub_id: str):
        if self.subscription_http is not None:
            return self.subscription_http, None
        if self.subscription_gone:
            return 404, None
        return 200, {"id": sub_id, "status": "active"}

    def list_databases(self, sub_id: str):
        if not self.database_applied:
            return 200, []
        return 200, [{"name": self.database_name, "status": self.database_status, "databaseId": "50"}]

    def get_database(self, sub_id: str, db_id: str):
        self.database_gets.append(str(db_id))
        if self.database_http is not None:
            return self.database_http, None
        if self.database_gone:
            return 404, None
        return 200, {"databaseId": db_id, "status": "active"}

    def delete_database(self, sub_id: str, db_id: str) -> None:
        self.cloud_deletes.append(f"{sub_id}/{db_id}")
        if self.delete_clears:
            self.database_applied = False
            self.database_gone = True

    def list_acl_rules(self) -> list:
        return [dict(row) for row in self.rules]

    def delete_acl_rule(self, rule_id: str) -> None:
        self.cloud_deletes.append(rule_id)
        if self.delete_clears:
            self.rules = [row for row in self.rules if str(row.get("id")) != str(rule_id)]

    def list_acl_roles(self) -> list:
        return [dict(row) for row in self.roles]

    def delete_acl_role(self, role_id: str) -> None:
        self.cloud_deletes.append(role_id)
        if self.delete_clears:
            self.roles = [row for row in self.roles if str(row.get("id")) != str(role_id)]

    def list_acl_users(self) -> list:
        return [dict(row) for row in self.users]

    def delete_acl_user(self, user_id: str) -> None:
        self.cloud_deletes.append(user_id)
        if self.delete_clears:
            self.users = [row for row in self.users if str(row.get("id")) != str(user_id)]

    def list_fixed_subscriptions(self) -> list:
        return [dict(row) for row in self.fixed]

    def list_fixed_subscriptions_checked(self) -> list:
        return self.list_fixed_subscriptions()

    def delete_fixed_subscription(self, sub_id: str) -> None:
        self.cloud_deletes.append(sub_id)
        if self.delete_clears:
            self.fixed = [row for row in self.fixed if str(row.get("id")) != str(sub_id)]
            self.ess_sub_gone = True

    def get_fixed_subscription(self, sub_id: str):
        if self.ess_sub_gone:
            return 404, None
        return 200, {"id": sub_id, "status": "active"}

    def list_fixed_databases(self, sub_id: str):
        if self.fixed_databases:
            return 200, [dict(row) for row in self.fixed_databases]
        if not self.ess_db_applied:
            return 200, []
        return 200, [{"name": self.ess_db_name, "status": "active", "databaseId": "60"}]

    def list_fixed_databases_complete(self, sub_id: str):
        if self.ess_db_gone:
            return 200, []
        return self.list_fixed_databases(sub_id)

    def delete_fixed_database(self, sub_id: str, db_id: str) -> None:
        self.cloud_deletes.append(f"{sub_id}/{db_id}")
        if self.delete_clears:
            self.fixed_databases = [
                row
                for row in self.fixed_databases
                if str(row.get("databaseId") or row.get("id")) != str(db_id)
            ]
            self.ess_db_applied = False
            self.ess_db_gone = True

    def get_fixed_database(self, sub_id: str, db_id: str):
        self.fixed_database_gets.append(str(db_id))
        if self.ess_db_gone:
            return 404, None
        return 200, {"databaseId": db_id, "name": self.ess_db_name, "status": "active",
                     "dataEvictionPolicy": self.eviction}


def ready(obj_id: str) -> dict:
    return {
        "metadata": {"uid": "test-uid", "finalizers": ["finalizer.managedresource.crossplane.io"]},
        "status": {
            "conditions": [{"type": "Ready", "status": "True", "reason": "Available"},
                           {"type": "Synced", "status": "True", "reason": "ReconcileSuccess"}],
            "atProvider": {"id": obj_id},
        }
    }


def deps_for(world: World, run_id: str = "123") -> qa_live.Deps:
    deps = qa_live.Deps(world, world, run_id, secrets=[API_KEY, SECRET_KEY, PAYMENT_ID])
    clock = Clock()
    deps._now = clock.now
    deps._sleep = clock.sleep
    deps.poll = 1.0
    deps.recovery_poll = 1.0
    deps.timeouts["recovery_hold"] = 0
    deps.log = lambda message: deps.lines.append(qa_live.redact(message, deps.secrets))
    return deps


class QaLiveTest(unittest.TestCase):
    def test_strict_essentials_uses_only_run_owned_cr_deletions(self):
        world = World()
        world.subs = [{"id": "9", "name": "xp-ci-unrelated"}]
        deps = deps_for(world)
        qa_live.strict_essentials(deps)
        self.assertEqual(world.cloud_deletes, [])
        self.assertEqual(names(world.subs), ["xp-ci-unrelated"])
        self.assertIn("strict essentials qualification passed; run-owned subscription inventory empty", deps.lines)
        self.assertLess(world.deleted.index((qa_live.ESS_DATABASE, "xp-ci-123")),
                        world.deleted.index((qa_live.ESS_SUBSCRIPTION, "xp-ci-123")))
        self.assertTrue(any("parent HTTP_OK" in line for line in deps.lines))
        self.assertTrue(any("eviction update verified volatile-lru -> allkeys-lru" in line for line in deps.lines))

    def test_eviction_update_fails_closed_on_changed_cloud_identity(self):
        world = World()
        original = world.get_fixed_database
        reads = 0
        def changed(sub, db):
            nonlocal reads
            reads += 1
            code, body = original(sub, db)
            if reads >= 3 and code == 200:
                body["databaseId"] = "different"
            return code, body
        world.get_fixed_database = changed
        deps = deps_for(world)
        with self.assertRaisesRegex(qa_live.SuiteFailure, "identity changed"):
            qa_live.strict_essentials(deps)
        self.assertEqual(world.cloud_deletes, [])
        self.assertNotIn((qa_live.ESS_SUBSCRIPTION, "xp-ci-123"), world.deleted)

    def test_eviction_update_requires_cloud_and_controller_agreement(self):
        world = World()
        original = world.get_fixed_database
        def stale(sub, db):
            code, body = original(sub, db)
            if body:
                body["dataEvictionPolicy"] = "volatile-lru"
            return code, body
        world.get_fixed_database = stale
        deps = deps_for(world)
        deps.timeouts["database"] = 2
        with self.assertRaisesRegex(qa_live.SuiteFailure, "did not converge"):
            qa_live.strict_essentials(deps)
        self.assertEqual(world.cloud_deletes, [])
        self.assertTrue(any("original qualification remains FAILED" in line for line in deps.lines))

    def test_strict_refuses_existing_essentials_without_mutation(self):
        world = World()
        world.fixed = [{"id": "9", "name": "xp-ci-unrelated"}]
        with self.assertRaises(qa_live.SuiteFailure):
            qa_live.strict_essentials(deps_for(world))
        self.assertEqual(world.applied, [])
        self.assertEqual(world.cloud_deletes, [])
        self.assertEqual(names(world.fixed), ["xp-ci-unrelated"])

    def test_strict_failure_keeps_failed_result_after_cr_recovery(self):
        world = World()
        world.get_fixed_database = lambda sub, db: (403, None)
        deps = deps_for(world)
        deps.timeouts["delete"] = 0
        with self.assertRaisesRegex(qa_live.SuiteFailure, "baseline"):
            qa_live.strict_essentials(deps)
        self.assertEqual(world.cloud_deletes, [])
        self.assertNotIn((qa_live.ESS_SUBSCRIPTION, "xp-ci-123"), world.deleted)
        self.assertTrue(any("recovery incomplete" in line for line in deps.lines))

    def test_strict_rejects_unsynced_or_missing_finalizer(self):
        obj = ready("200")
        obj["status"]["conditions"][1]["status"] = "False"
        with self.assertRaises(qa_live.SuiteFailure):
            qa_live.strict_identity(obj, "subscription")
        obj = ready("200")
        obj["metadata"]["finalizers"] = []
        with self.assertRaises(qa_live.SuiteFailure):
            qa_live.strict_identity(obj, "subscription")
        obj["metadata"]["finalizers"] = ["unrelated.example/finalizer"]
        with self.assertRaises(qa_live.SuiteFailure):
            qa_live.strict_identity(obj, "subscription")

    def test_strict_rejects_wrong_plan_before_creating_cloud_resource(self):
        world = World()
        original_apply = world.apply
        def apply(manifest):
            original_apply(manifest)
            if manifest["kind"] == "EssentialsPlan":
                world.objects[(qa_live.PLAN, manifest["metadata"]["name"])]["status"]["atProvider"]["cloudProvider"] = "AWS"
        world.apply = apply
        with self.assertRaisesRegex(qa_live.SuiteFailure, "GCP"):
            qa_live.strict_essentials(deps_for(world))
        self.assertFalse(any(m["kind"] == "Subscription" for m in world.applied))
        self.assertEqual(world.cloud_deletes, [])

    def test_strict_refuses_unverified_endpoint_before_creating_deps(self):
        with patch.object(qa_live, "load_credentials", return_value={
            "api_key": "a", "secret_key": "b", "url": "https://unknown.example/v1"
        }), patch.object(qa_live, "build_deps") as build:
            self.assertEqual(qa_live.main(["--strict-essentials"]), 1)
            build.assert_not_called()

    def test_qa_origin_match_requires_exact_https_origin_and_v1_path(self):
        safe_hash = hashlib.sha256(b"https://qa.example.invalid").hexdigest()
        with patch.dict("os.environ", {"QA_VERIFIED_ORIGIN_HASHES": safe_hash}):
            self.assertTrue(qa_live.matches_expected_qa_origin("https://qa.example.invalid/v1"))
            for url in ("http://qa.example.invalid/v1", "https://other.invalid/v1",
                        "https://qa.example.invalid/v2", "https://qa.example.invalid/v1?x=1",
                        "https://qa.example.invalid.evil/v1", "https://qa.example.invalid:444/v1"):
                self.assertFalse(qa_live.matches_expected_qa_origin(url))

    def test_cloud_request_rejects_redirect_without_following_it(self):
        paths = []
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                paths.append(self.path)
                self.send_response(302)
                self.send_header("Location", "/other")
                self.end_headers()
            def log_message(self, *args):
                pass
        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        worker = threading.Thread(target=server.serve_forever, daemon=True)
        worker.start()
        try:
            cloud = qa_live.RedisCloud(f"http://127.0.0.1:{server.server_port}", "a", "b")
            self.assertEqual(cloud.request("GET", "/first")[0], 302)
            self.assertEqual(paths, ["/first"])
        finally:
            server.shutdown()
            server.server_close()
            worker.join()

    def test_cloud_transport_failure_does_not_expose_endpoint(self):
        cloud = qa_live.RedisCloud("https://private.example.invalid/v1", "a", "b")
        with patch("urllib.request.build_opener") as opener:
            opener.return_value.open.side_effect = OSError("private.example.invalid")
            with self.assertRaises(qa_live.SuiteFailure) as caught:
                cloud.request("GET", "/fixed/plans")
            self.assertNotIn("private.example.invalid", str(caught.exception))

    def test_preflight_is_read_only_and_never_uses_kubernetes(self):
        class PreflightCloud:
            def __init__(self, *args):
                pass
            def request(self, method, path):
                if method != "GET":
                    raise AssertionError("preflight attempted a Cloud write")
                if path == "/tasks":
                    return 200, {"tasks": []}
                return 200, {"plans": [{"id": 5, "provider": "GCP", "region": "us-central1",
                                        "name": "1GB", "size": 1, "sizeMeasurementUnit": "GB",
                                        "supportReplication": False, "supportDataPersistence": False}]}
            def list_fixed_subscriptions_checked(self):
                return []
        with patch.object(qa_live, "load_credentials", return_value={
            "api_key": "a", "secret_key": "b", "url": "https://qa.example.invalid/v1"
        }), patch.object(qa_live, "matches_expected_qa_origin", return_value=True), \
             patch.object(qa_live, "RedisCloud", PreflightCloud), patch.object(qa_live, "build_deps") as build:
            self.assertEqual(qa_live.main(["--preflight-only"]), 0)
            build.assert_not_called()

    def test_task_diagnostic_outputs_only_safe_status_and_time(self):
        rows = {
            "sensitive-task-id": {"commandType": "fixedDatabaseDeleteRequest", "status": "processing-in-progress", "timestamp": "2026-11-23T22:30:00Z", "description": "sensitive-description"},
            "old-id": {"commandType": "fixedDatabaseDeleteRequest", "status": "processing-error", "timestamp": "2026-11-22T22:00:00Z"},
            "bad-id": {"commandType": "fixedDatabaseDeleteRequest", "status": "injected-private-value", "timestamp": "invalid"},
        }
        lines = []
        credentials = {"api_key": "private-account-key", "secret_key": "private-secret", "url": "https://qa.example.invalid/v1"}
        encoded = base64.b64encode(b"k" * 32).decode()
        with patch.dict("os.environ", {"QA_ACCOUNT_FINGERPRINT_KEY": encoded}), \
             patch.object(qa_live, "matches_expected_qa_origin", return_value=True), \
             patch.object(qa_capture_task_baseline, "task_rows", return_value=rows), \
             patch.object(qa_live.RedisCloud, "request", return_value=(200, {"accountId": 9876})):
            qa_live.task_diagnostic(credentials, datetime(2026, 11, 23, 23, 0, tzinfo=timezone.utc), lines.append)
        text = "\n".join(lines)
        self.assertIn("task count=1; invalid timestamps=1", text)
        self.assertIn("status=processing-in-progress", text)
        self.assertIn(hmac.new(b"k" * 32, b"9876", hashlib.sha256).hexdigest(), text)
        for secret in ("sensitive-task-id", "sensitive-description", "private-account-key", "private-secret", "injected-private-value"):
            self.assertNotIn(secret, text)

    def test_task_diagnostic_redacts_foreign_module_failure(self):
        class ForeignFailure(Exception):
            pass
        credentials = {"api_key": "a", "secret_key": "b", "url": "https://qa.example.invalid/v1"}
        with patch.object(qa_live, "matches_expected_qa_origin", return_value=True), \
             patch.object(qa_capture_task_baseline, "qa_live", SimpleNamespace(SuiteFailure=ForeignFailure)), \
             patch.object(qa_capture_task_baseline, "task_rows", side_effect=ForeignFailure("private detail")):
            with self.assertRaises(qa_live.SuiteFailure) as caught:
                qa_live.task_diagnostic(credentials)
        self.assertNotIn("private detail", str(caught.exception))

    def test_strict_mode_requires_verified_origin_before_invocation(self):
        with patch.object(qa_live, "load_credentials", return_value={
            "api_key": "a", "secret_key": "b", "url": "https://qa.example.invalid/v1"
        }), patch.object(qa_live, "matches_expected_qa_origin", return_value=True), \
             patch.object(qa_live, "verify_checkpoint_artifact") as checkpoint, \
             patch.object(qa_live, "build_deps") as build, \
             patch.object(qa_live, "strict_essentials") as strict:
            self.assertEqual(qa_live.main(["--strict-essentials"]), 0)
            checkpoint.assert_called_once()
            strict.assert_called_once_with(build.return_value)

    def test_strict_requires_persisted_artifact_for_exact_run_and_commit(self):
        key = b"x" * 32
        creds = {"api_key": "account", "url": "https://qa.example.invalid/v1"}
        env = {"GITHUB_ACTIONS": "true", "GITHUB_REPOSITORY": "RedisLabs/crossplane-provider-rediscloud",
               "GITHUB_RUN_ID": "123", "GITHUB_RUN_ATTEMPT": "2", "GITHUB_SHA": "a" * 40,
               "QA_OWNERSHIP_ARTIFACT_ID": "456", "QA_CHECKPOINT_KEY": base64.b64encode(key).decode()}
        intent = {"kind": "strict", "commit": "a" * 40,
                  "originHash": hashlib.sha256(b"https://qa.example.invalid").hexdigest(),
                  "credentialBinding": hmac.new(key, b"account", hashlib.sha256).hexdigest()}
        with tempfile.TemporaryDirectory() as tmp:
            source = Path(tmp) / "ownership.enc"
            source.write_text("ciphertext")
            env["QA_OWNERSHIP_ARTIFACT_PATH"] = str(source)
            def decrypt(command, **kwargs):
                Path(command[3] + ".resolved").write_text(json.dumps(intent))
            with patch.dict("os.environ", env), patch.object(qa_live.subprocess, "run", side_effect=decrypt) as run:
                qa_live.verify_checkpoint_artifact("123-2", creds)
                with self.assertRaises(qa_live.SuiteFailure):
                    qa_live.verify_checkpoint_artifact("123-1", creds)
                intent["kind"] = "rehearsal"
                with self.assertRaises(qa_live.SuiteFailure):
                    qa_live.verify_checkpoint_artifact("123-2", creds)
                intent["kind"] = "strict"
                intent["commit"] = "b" * 40
                with self.assertRaises(qa_live.SuiteFailure):
                    qa_live.verify_checkpoint_artifact("123-2", creds)
                intent["commit"] = "a" * 40
                intent["originHash"] = "0" * 64
                with self.assertRaises(qa_live.SuiteFailure):
                    qa_live.verify_checkpoint_artifact("123-2", creds)
                intent["originHash"] = hashlib.sha256(b"https://qa.example.invalid").hexdigest()
                intent["credentialBinding"] = "0" * 64
                with self.assertRaises(qa_live.SuiteFailure):
                    qa_live.verify_checkpoint_artifact("123-2", creds)
            with patch.dict("os.environ", dict(env, QA_OWNERSHIP_ARTIFACT_ID="")), \
                 patch.object(qa_live.subprocess, "run") as run:
                with self.assertRaises(qa_live.SuiteFailure):
                    qa_live.verify_checkpoint_artifact("123-2", creds)
                run.assert_not_called()

    def test_strict_writer_requires_successful_checkpoint_job(self):
        workflow = (REPO / ".github/workflows/qa-live.yaml").read_text()
        writer = workflow[workflow.index("  qa-live:"):]
        self.assertIn("needs: prepare-strict", writer)
        self.assertIn("needs.prepare-strict.result == 'success'", writer)
        self.assertIn("!cancelled()", writer)
        self.assertLess(writer.index("Retrieve persisted ownership intent before Cloud writes"),
                        writer.index("Create and delete the QA objects"))

    def test_strict_recovers_only_its_exact_resources_and_still_fails(self):
        world = World()
        original_delete = world.delete
        def stuck_cr(resource, name, namespace=None):
            if resource in (qa_live.ESS_DATABASE, qa_live.ESS_SUBSCRIPTION):
                world.deleted.append((resource, name))
                return
            original_delete(resource, name, namespace)
        world.delete = stuck_cr
        deps = deps_for(world)
        deps.timeouts["delete"] = 0
        with self.assertRaisesRegex(qa_live.SuiteFailure, "database deletion timed out"):
            qa_live.strict_essentials(deps)
        self.assertEqual(world.cloud_deletes, [])
        self.assertNotIn((qa_live.ESS_SUBSCRIPTION, "xp-ci-123"), world.deleted)
        self.assertTrue(any("parent preserved" in line for line in deps.lines))

    def test_strict_holds_controller_and_credentials_while_cleanup_is_unresolved(self):
        world = World()
        original_delete = world.delete
        def stuck_db(resource, name, namespace=None):
            if resource == qa_live.ESS_DATABASE:
                world.deleted.append((resource, name))
                return
            original_delete(resource, name, namespace)
        world.delete = stuck_db
        deps = deps_for(world)
        deps.timeouts["delete"] = 0
        deps.timeouts["recovery_hold"] = 2
        with self.assertRaisesRegex(qa_live.SuiteFailure, "database deletion timed out"):
            qa_live.strict_essentials(deps)
        self.assertGreaterEqual(deps.now(), 2)
        self.assertTrue(any("RECOVERY HOLD:" in line for line in deps.lines))
        self.assertTrue(any("RECOVERY HOLD TIMED OUT" in line for line in deps.lines))
        self.assertNotIn((qa_live.ESS_SUBSCRIPTION, "xp-ci-123"), world.deleted)
        self.assertNotIn(("secret", "provider-secret"), world.deleted)
        self.assertEqual(world.cloud_deletes, [])

    def test_cr_recovery_retry_can_finish_but_never_turns_failure_into_pass(self):
        world = World()
        original_delete = world.delete
        calls = []
        def delayed_delete(resource, name, namespace=None):
            if resource == qa_live.ESS_DATABASE:
                calls.append(1)
                if len(calls) <= 2:
                    world.deleted.append((resource, name))
                    return
            original_delete(resource, name, namespace)
        world.delete = delayed_delete
        deps = deps_for(world)
        deps.timeouts["delete"] = 0
        deps.timeouts["recovery_hold"] = 2
        with self.assertRaisesRegex(qa_live.SuiteFailure, "database deletion timed out"):
            qa_live.strict_essentials(deps)
        self.assertTrue(any("RECOVERY HOLD:" in line for line in deps.lines))
        self.assertGreaterEqual(deps.now(), 1)
        self.assertTrue(any("recovery complete; original qualification remains FAILED" in line for line in deps.lines))
        self.assertIn((qa_live.ESS_SUBSCRIPTION, "xp-ci-123"), world.deleted)
        self.assertEqual(world.cloud_deletes, [])

    def test_hold_deadline_caps_each_cleanup_poll(self):
        world = World()
        original_delete = world.delete
        def stuck_db(resource, name, namespace=None):
            if resource == qa_live.ESS_DATABASE:
                world.deleted.append((resource, name))
                return
            original_delete(resource, name, namespace)
        world.delete = stuck_db
        deps = deps_for(world)
        deps.timeouts["delete"] = 5
        deps.timeouts["recovery_hold"] = 2
        with self.assertRaises(qa_live.SuiteFailure):
            qa_live.strict_essentials(deps)
        self.assertEqual(deps.now(), 7)
        self.assertTrue(any("RECOVERY HOLD TIMED OUT" in line for line in deps.lines))

    def test_recovery_preserves_parent_if_other_children_remain(self):
        world = World()
        deps = deps_for(world)
        def missing_event(uid, label):
            if label == "database":
                raise qa_live.SuiteFailure("database event missing")
        world.require_deleted_event = missing_event
        world.list_fixed_databases_complete = lambda sub: (200, [{"databaseId": 61, "name": "foreign"}])
        with self.assertRaisesRegex(qa_live.SuiteFailure, "event missing"):
            qa_live.strict_essentials(deps)
        self.assertNotIn((qa_live.ESS_SUBSCRIPTION, "xp-ci-123"), world.deleted)
        self.assertEqual(world.cloud_deletes, [])

    def test_recovery_finalizes_parent_cr_when_exact_parent_is_already_absent(self):
        world = World()
        world.ess_sub_gone = True
        state = qa_live.State("xp-ci-123")
        state.essentials_name = state.run_name
        state.essentials_id = "200"
        state.essentials_database_name = state.run_name
        state.essentials_database_deleted = True
        qa_live.strict_recover(deps_for(world), state)
        self.assertIn((qa_live.ESS_SUBSCRIPTION, "xp-ci-123"), world.deleted)
        self.assertEqual(world.cloud_deletes, [])

    def test_recovery_refuses_parent_cr_delete_on_bad_read_or_identity(self):
        for bad_read in (True, False):
            world = World()
            world.fixed = [{"id": "999", "name": "xp-ci-123", "status": "active"}]
            if bad_read:
                world.get_fixed_subscription = lambda sub: (500, None)
            state = qa_live.State("xp-ci-123")
            state.essentials_name = state.run_name
            state.essentials_id = "200"
            state.essentials_database_name = state.run_name
            state.essentials_database_deleted = True
            qa_live.strict_recover(deps_for(world), state)
            self.assertNotIn((qa_live.ESS_SUBSCRIPTION, "xp-ci-123"), world.deleted)
            self.assertEqual(world.cloud_deletes, [])

    def test_recovery_refuses_parent_without_matching_cloud_identity_even_without_child(self):
        world = World()
        world.fixed = [{"id": "999", "name": "xp-ci-123", "status": "active"}]
        world.objects[(qa_live.ESS_SUBSCRIPTION, "xp-ci-123")] = ready("200")
        state = qa_live.State("xp-ci-123")
        state.essentials_name = state.run_name
        state.essentials_id = "200"
        self.assertFalse(qa_live.strict_recover(deps_for(world), state))
        self.assertNotIn((qa_live.ESS_SUBSCRIPTION, state.run_name), world.deleted)

    def test_recovery_preserves_cr_until_identity_is_independently_matched(self):
        world = World()
        state = qa_live.State("xp-ci-123")
        state.essentials_name = state.run_name
        deps = deps_for(world)
        self.assertFalse(qa_live.strict_recover(deps, state))
        self.assertNotIn((qa_live.ESS_SUBSCRIPTION, state.run_name), world.deleted)
        world.fixed = [{"id": "200", "name": state.run_name, "status": "active"}]
        world.objects[(qa_live.ESS_SUBSCRIPTION, state.run_name)] = ready("200")
        self.assertTrue(qa_live.strict_recover(deps, state))
        self.assertEqual(state.essentials_id, "200")
        self.assertIn((qa_live.ESS_SUBSCRIPTION, state.run_name), world.deleted)

    def test_recovery_accepts_id_alias_on_exact_database_read(self):
        world = World()
        world.fixed = [{"id": "200", "name": "xp-ci-123", "status": "active"}]
        world.objects[(qa_live.ESS_SUBSCRIPTION, "xp-ci-123")] = ready("200")
        world.objects[(qa_live.ESS_DATABASE, "xp-ci-123")] = ready("200/60")
        world.get_fixed_database = lambda sub, db: (404, None) if world.ess_db_gone else (200, {"id": 60, "name": "xp-ci-123-db"})
        state = qa_live.State("xp-ci-123")
        state.essentials_name = state.run_name
        state.essentials_id = "200"
        state.essentials_database_name = state.run_name
        state.essentials_database_id = "60"
        self.assertTrue(qa_live.strict_recover(deps_for(world), state))
        self.assertEqual(world.cloud_deletes, [])

    def test_strict_waits_for_delayed_database_absence(self):
        world = World()
        get_db = world.get_fixed_database
        attempts = []
        def delayed(sub_id, db_id):
            if world.ess_db_gone and len(attempts) < 2:
                attempts.append(1)
                return 200, {}
            return get_db(sub_id, db_id)
        world.get_fixed_database = delayed
        deps = deps_for(world)
        qa_live.strict_essentials(deps)
        self.assertEqual(len(attempts), 2)
        self.assertTrue(any("parent HTTP_OK" in line for line in deps.lines))

    def test_strict_rejects_database_with_wrong_parent(self):
        world = World()
        original_apply = world.apply
        def apply(manifest):
            original_apply(manifest)
            if manifest["kind"] == "Database" and manifest["apiVersion"].startswith("essentials."):
                world.objects[(qa_live.ESS_DATABASE, manifest["metadata"]["name"])]["status"]["atProvider"]["id"] = "999/60"
        world.apply = apply
        deps = deps_for(world)
        with self.assertRaisesRegex(qa_live.SuiteFailure, "parent differs"):
            qa_live.strict_essentials(deps)
        self.assertFalse(any("qualification passed" in line for line in deps.lines))
        self.assertNotIn((qa_live.ESS_DATABASE, "xp-ci-123"), world.deleted)
        self.assertNotIn((qa_live.ESS_SUBSCRIPTION, "xp-ci-123"), world.deleted)
        self.assertEqual(world.cloud_deletes, [])

    def test_database_inventory_requires_empty_authenticated_last_page(self):
        cloud = qa_live.RedisCloud("https://example.invalid/v1", "a", "b")
        calls = []
        def request(method, path):
            calls.append(path)
            rows = [{"name": "first"}] if "offset=0" in path else []
            return 200, {"subscription": {"databases": rows}}
        cloud.request = request
        code, rows = cloud.list_fixed_databases_complete("7")
        self.assertEqual(code, 200)
        self.assertEqual(rows, [{"name": "first"}])
        self.assertEqual(len(calls), 2)
        cloud.request = lambda method, path: (403, None)
        with self.assertRaises(qa_live.SuiteFailure):
            cloud.list_fixed_databases_complete("7")

    def test_controller_deletion_event_must_match_original_uid(self):
        world = World()
        deps = deps_for(world)
        cluster = qa_live.KubectlCluster("kubectl", qa_live.ALLOWED_CONTEXT, {}, deps)
        events = [{"involvedObject": {"uid": "other"}, "reason": "DeletedExternalResource", "type": "Normal"}]
        cluster._run = lambda args: SimpleNamespace(stdout=json.dumps({"items": events}))
        with self.assertRaises(qa_live.SuiteFailure):
            cluster.require_deleted_event("original", "database")
        events.append({"involvedObject": {"uid": "original"}, "reason": "DeletedExternalResource", "type": "Normal"})
        cluster.require_deleted_event("original", "database")

    def test_existing_prefix_does_not_create(self):
        world = World()
        world.delete_clears = False
        world.subs = [{"id": "9", "name": "xp-ci-old", "status": "active"}]
        deps = deps_for(world)
        deps.timeouts["delete"] = 0
        with self.assertRaises(qa_live.SuiteFailure) as caught:
            qa_live.run(deps)
        self.assertIn("still present", str(caught.exception))
        self.assertEqual(world.applied, [])
        self.assertFalse(world.installed)
        self.assertEqual(world.cloud_deletes, ["9"])

    def test_failed_database_deletes_subscription(self):
        world = World()
        world.fail_database = True
        deps = deps_for(world)
        with self.assertRaises(qa_live.SuiteFailure) as caught:
            qa_live.run(deps)
        self.assertIn("database step failed", str(caught.exception))
        self.assertIn((qa_live.SUBSCRIPTION, "xp-ci-123"), world.deleted)
        self.assertTrue(any(item["kind"] == "Subscription" for item in world.applied))
        self.assertFalse(any(item["kind"] == "Database" for item in world.applied))

    def test_success_check_requires_active_keys_and_404(self):
        ok = qa_live.success_problems("active", "active", ["public_endpoint", "private_endpoint"], 404, 404)
        self.assertEqual(ok, [])
        self.assertTrue(qa_live.success_problems("pending", "active", ["public_endpoint", "private_endpoint"], 404, 404))
        self.assertTrue(qa_live.success_problems("active", "pending", ["public_endpoint", "private_endpoint"], 404, 404))
        self.assertTrue(any("public_endpoint" in item for item in qa_live.success_problems("active", "active", ["private_endpoint"], 404, 404)))
        self.assertTrue(any("private_endpoint" in item for item in qa_live.success_problems("active", "active", ["public_endpoint"], 404, 404)))
        self.assertTrue(any("database delete HTTP 200" in item for item in qa_live.success_problems("active", "active", ["public_endpoint", "private_endpoint"], 200, 404)))
        self.assertTrue(any("subscription delete HTTP 200" in item for item in qa_live.success_problems("active", "active", ["public_endpoint", "private_endpoint"], 404, 200)))

    def test_run_fails_when_secret_key_is_missing_and_still_deletes(self):
        world = World()
        world.secret_keys = ["public_endpoint"]
        deps = deps_for(world)
        with self.assertRaises(qa_live.SuiteFailure) as caught:
            qa_live.run(deps)
        self.assertIn("private_endpoint", str(caught.exception))
        self.assertIn((qa_live.SUBSCRIPTION, "xp-ci-123"), world.deleted)
        self.assertNotIn(PAYMENT_ID, "\n".join(deps.lines))
        self.assertNotIn(API_KEY, "\n".join(deps.lines))

    def test_run_fails_when_delete_is_not_404(self):
        world = World()
        world.database_http = 200
        world.subscription_http = 200
        deps = deps_for(world)
        for name in deps.timeouts:
            deps.timeouts[name] = 0
        with self.assertRaises(qa_live.SuiteFailure) as caught:
            qa_live.run(deps)
        self.assertIn("HTTP 200", str(caught.exception))

    def test_run_succeeds_and_logs_key_names_only(self):
        world = World()
        deps = deps_for(world)
        qa_live.run(deps)
        text = "\n".join(deps.lines)
        self.assertIn("secret keys private_endpoint,public_endpoint", deps.lines)
        self.assertIn("qa live ok", deps.lines)
        self.assertIn("ACL role and user xp-ci-123 created and gone", deps.lines)
        self.assertIn("essentials xp-ci-123 created and gone", deps.lines)
        self.assertNotIn(PAYMENT_ID, text)
        self.assertNotIn(PAYMENT_NUMBER, text)
        self.assertNotIn(PLAN_NUMBER, text)
        self.assertNotIn(SECRET_KEY, text)
        passwords = [
            item["stringData"]["password"]
            for item in world.applied
            if item["kind"] == "Secret" and item["metadata"]["name"].endswith("-acl-password")
        ]
        self.assertEqual(len(passwords), 1)
        self.assertNotIn(passwords[0], text)
        role = next(item for item in world.applied if item["kind"] == "Role")
        bound = role["spec"]["forProvider"]["rule"][0]["database"][0]
        self.assertEqual(bound, {"subscription": 100, "database": 50})
        self.assertIn("50", world.database_gets)
        self.assertNotIn("100/50", world.database_gets)
        self.assertIn("60", world.fixed_database_gets)
        self.assertNotIn("200/60", world.fixed_database_gets)
        self.assertNotIn("100/50", text)
        self.assertNotIn("200/60", text)
        essentials = next(
            item for item in world.applied
            if item["kind"] == "Subscription" and item["apiVersion"].startswith("essentials.")
        )
        provider = essentials["spec"]["forProvider"]
        self.assertEqual(provider["planId"], 7)
        self.assertEqual(provider["paymentMethodId"], 4242)
        self.assertTrue(world.subscription_gone)
        self.assertTrue(world.database_gone)
        self.assertTrue(world.ess_sub_gone)
        self.assertTrue(world.ess_db_gone)
        self.assertEqual(names(world.subs), [])
        self.assertEqual(names(world.fixed), [])
        self.assertEqual(names(world.roles), [])
        self.assertEqual(names(world.users), [])
        deleted = world.deleted
        self.assertLess(deleted.index((qa_live.USER, "xp-ci-123")), deleted.index((qa_live.ROLE, "xp-ci-123")))
        self.assertLess(deleted.index((qa_live.ROLE, "xp-ci-123")), deleted.index((qa_live.ACL, "xp-ci-123")))
        self.assertLess(deleted.index((qa_live.SUBSCRIPTION, "xp-ci-123")), deleted.index((qa_live.ESS_SUBSCRIPTION, "xp-ci-123")))
        self.assertLess(
            deleted.index((qa_live.ESS_DATABASE, "xp-ci-123")),
            deleted.index((qa_live.ESS_SUBSCRIPTION, "xp-ci-123")),
        )

    def test_failed_essentials_still_deletes_the_subscription(self):
        world = World()
        world.fail_essentials = True
        deps = deps_for(world)
        with self.assertRaises(qa_live.SuiteFailure) as caught:
            qa_live.run(deps)
        self.assertIn("essentials step failed", str(caught.exception))
        self.assertIn((qa_live.ESS_SUBSCRIPTION, "xp-ci-123"), world.deleted)
        self.assertTrue(world.subscription_gone)
        self.assertTrue(world.database_gone)

    def test_foreign_essentials_subscription_stops_before_create(self):
        world = World()
        world.fixed = [{"id": "9", "name": "kept", "status": "active"}]
        deps = deps_for(world)
        with self.assertRaises(qa_live.SuiteFailure) as caught:
            qa_live.run(deps)
        self.assertIn("already has an essentials subscription", str(caught.exception))
        self.assertFalse(any(
            item["kind"] == "Subscription" and str(item.get("apiVersion", "")).startswith("essentials.")
            for item in world.applied
        ))
        self.assertTrue(world.subscription_gone)
        self.assertTrue(world.database_gone)
        self.assertNotIn("9", "\n".join(deps.lines))

    def test_leftover_essentials_blocks_a_second_create(self):
        world = World()
        world.delete_clears = False
        world.fixed = [{"id": "8", "name": "xp-ci-old", "status": "active"}]
        deps = deps_for(world)
        deps.timeouts["delete"] = 0
        with self.assertRaises(qa_live.SuiteFailure) as caught:
            qa_live.run(deps)
        self.assertIn("still present", str(caught.exception))
        self.assertEqual(world.applied, [])
        self.assertIn("8", world.cloud_deletes)

    def test_leftover_essentials_deletes_database_before_subscription(self):
        world = World()
        world.fixed = [{"id": "8", "name": "xp-ci-old", "status": "active"}]
        world.fixed_databases = [{"databaseId": "61", "name": "xp-ci-old-db", "status": "active"}]
        deps = deps_for(world)
        qa_live.cleanup_only(deps)
        self.assertEqual(world.cloud_deletes, ["8/61", "8"])
        self.assertEqual(world.fixed, [])
        self.assertEqual(world.fixed_databases, [])

    def test_fixed_database_list_reads_the_subscription_object(self):
        body = {"subscription": {"subscriptionId": 1, "databases": [{"name": "db", "status": "active"}]}}
        self.assertEqual(qa_live.fixed_database_rows(body), [{"name": "db", "status": "active"}])
        self.assertEqual(qa_live.named_rows({"roles": [{"name": "r"}]}, "roles"), [{"name": "r"}])

    def test_cleanup_only_does_not_create(self):
        world = World()
        world.subs = [{"id": "9", "name": "xp-ci-old", "status": "active"}]
        deps = deps_for(world)
        qa_live.cleanup_only(deps)
        self.assertEqual(world.applied, [])
        self.assertFalse(world.installed)
        self.assertEqual(names(world.subs), [])

    def test_cleanup_only_refuses_when_delete_does_not_clear(self):
        world = World()
        world.delete_clears = False
        world.subs = [{"id": "9", "name": "xp-ci-old", "status": "active"}]
        deps = deps_for(world)
        deps.timeouts["delete"] = 0
        with self.assertRaises(qa_live.SuiteFailure):
            qa_live.cleanup_only(deps)
        self.assertEqual(world.applied, [])

    def test_database_number_uses_the_database_segment(self):
        self.assertEqual(qa_live.database_number("50"), "50")
        self.assertEqual(qa_live.database_number("100/50"), "50")
        self.assertEqual(qa_live.observed_database_id({"databaseId": 60}, "200/60"), "60")
        self.assertEqual(qa_live.observed_database_id({}, "200/60"), "60")
        with self.assertRaises(qa_live.SuiteFailure):
            qa_live.database_number("100/50/1")
        with self.assertRaises(qa_live.SuiteFailure):
            qa_live.database_number("subscription/database")

    def test_context_guard(self):
        qa_live.assert_safe_context("kind-local-dev")
        with self.assertRaises(qa_live.SuiteFailure):
            qa_live.assert_safe_context("gke_redislabs-redisvpc-dev-238506_us-east1_sm-qa")

    def test_normalize_credentials_and_secret_names(self):
        got = qa_live.normalize_credentials(
            {"api_key": "a", "api_secret_key": "b", "base_url": "https://example.invalid/v1/", "account_id": "nope"}
        )
        self.assertEqual(got, {"api_key": "a", "secret_key": "b", "url": "https://example.invalid/v1"})
        names = qa_live.secret_key_names({"data": {"public_endpoint": "sekret", "private_endpoint": "also"}})
        self.assertEqual(names, ["private_endpoint", "public_endpoint"])
        self.assertNotIn("sekret", names)

    def test_choose_cidr_skips_held_and_used(self):
        used = {"10.20.3.0/24"}
        cidr = qa_live.choose_cidr("1", used)
        self.assertNotIn(cidr, qa_live.HELD_CIDRS)
        self.assertNotIn(cidr, used)
        self.assertTrue(cidr.startswith("10."))

    def test_workflow_is_not_a_pull_request(self):
        text = (REPO / ".github/workflows/qa-live.yaml").read_text()
        self.assertNotIn("pull_request", text)
        self.assertIn("schedule:", text)
        self.assertIn("workflow_dispatch:", text)
        self.assertIn("17 6 * * *", text)
        self.assertNotIn("17 4 * * *", text)
        self.assertIn("secrets.UPTEST_CLOUD_CREDENTIALS", text)
        self.assertNotIn("api_key:", text)
        self.assertNotIn("secret_key:", text)


def names(rows: list) -> list[str]:
    return [str(row.get("name")) for row in rows]


if __name__ == "__main__":
    unittest.main()

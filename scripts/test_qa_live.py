#!/usr/bin/env python3
"""Hermetic tests for the QA create-and-delete script."""

from __future__ import annotations

import unittest
from pathlib import Path

import qa_live

REPO = Path(__file__).resolve().parents[1]
PAYMENT_ID = "should-not-print"
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
        self.objects = {}
        self.installed = False
        self.delete_clears = True
        self.fail_database = False
        self.secret_keys = ["private_endpoint", "public_endpoint"]
        self.database_status = "active"
        self.subscription_status = "active"
        self.database_http = None
        self.subscription_http = None
        self.subscription_gone = False
        self.database_gone = False
        self.database_applied = False

    def install_provider_config(self) -> None:
        self.installed = True

    def apply(self, manifest: dict) -> None:
        if manifest["kind"] == "Database" and self.fail_database:
            raise qa_live.SuiteFailure("database step failed")
        self.applied.append(manifest)
        name = manifest["metadata"]["name"]
        kind = manifest["kind"]
        if kind == "Subscription":
            self.subs.append({"id": "100", "name": name, "status": self.subscription_status})
            self.objects[(qa_live.SUBSCRIPTION, name)] = ready("100")
        elif kind == "Rule":
            self.rules.append({"name": name, "status": "active", "acl": qa_live.ACL_RULE})
            self.objects[(qa_live.ACL, name)] = ready("1")
        elif kind == "Database":
            self.database_applied = True
            self.database_name = manifest["spec"]["forProvider"]["name"]
            self.objects[(qa_live.DATABASE, name)] = ready("50")
        elif kind == "PaymentMethod":
            self.objects[(qa_live.PAYMENT, name)] = ready("1")
        elif kind == "EssentialsPlan":
            self.objects[(qa_live.PLAN, name)] = ready("1")
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
        self.objects.pop((resource, name), None)

    def get(self, resource: str, name: str, namespace: str | None = None):
        return self.objects.get((resource, name))

    def secret_key_names(self, namespace: str, name: str) -> list[str]:
        return list(self.secret_keys)

    def payment_method_id(self) -> str:
        return PAYMENT_ID

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
        if self.database_http is not None:
            return self.database_http, None
        if self.database_gone:
            return 404, None
        return 200, {"databaseId": db_id, "status": "active"}

    def list_acl_rules(self) -> list:
        return [dict(row) for row in self.rules]


def ready(obj_id: str) -> dict:
    return {
        "status": {
            "conditions": [{"type": "Ready", "status": "True", "reason": "Available"}],
            "atProvider": {"id": obj_id},
        }
    }


def deps_for(world: World, run_id: str = "123") -> qa_live.Deps:
    deps = qa_live.Deps(world, world, run_id, secrets=[API_KEY, SECRET_KEY, PAYMENT_ID])
    clock = Clock()
    deps._now = clock.now
    deps._sleep = clock.sleep
    deps.poll = 1.0
    deps.log = lambda message: deps.lines.append(qa_live.redact(message, deps.secrets))
    return deps


class QaLiveTest(unittest.TestCase):
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
        self.assertIn("secret keys private_endpoint,public_endpoint", deps.lines)
        self.assertIn("qa live ok", deps.lines)
        self.assertNotIn(PAYMENT_ID, "\n".join(deps.lines))
        self.assertNotIn(SECRET_KEY, "\n".join(deps.lines))
        self.assertTrue(world.subscription_gone)
        self.assertTrue(world.database_gone)
        self.assertEqual(names(world.subs), [])

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

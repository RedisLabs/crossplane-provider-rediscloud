"""Test-only adapter for the strict harness and its in-memory World.

No provider, Kubernetes API, Cloud API or lock is started. Fixture URLs are
reserved and are not evidence of an actual hosted run.
"""

from pathlib import Path
import sys
from unittest.mock import patch

from contract import Evidence, StepReceipt

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / "scripts"))
import qa_live  # noqa: E402
import test_qa_live  # noqa: E402


FIXTURE_URL = "https://fixture.invalid/crossplane-fake-world"
FIXTURE_REVISION = "0" * 40
EXPECTED_STEPS = (
    "admission_and_clean_baseline", "create_subscription", "read_subscription_active",
    "create_database", "read_database_active", "update_eviction",
    "read_updated_eviction_same_identity", "delete_database",
    "read_database_absent_parent_present", "delete_subscription",
    "read_subscription_absent", "complete_clean_inventory",
)


class TracingWorld(test_qa_live.World):
    def __init__(self):
        super().__init__()
        self.events = []

    def list_fixed_subscriptions_checked(self):
        rows = super().list_fixed_subscriptions_checked()
        if not rows:
            self.events.append("inventory_empty")
        return rows

    def apply(self, manifest):
        super().apply(manifest)
        api, kind = manifest.get("apiVersion", ""), manifest.get("kind")
        if api.startswith("essentials.") and kind in {"Subscription", "Database"}:
            self.events.append("create_" + kind.lower())

    def get_fixed_subscription(self, sub_id):
        result = super().get_fixed_subscription(sub_id)
        if str(sub_id) == "200" and result[0] in (200, 404):
            self.events.append("parent_" + str(result[0]))
        return result

    def get_fixed_database(self, sub_id, db_id):
        result = super().get_fixed_database(sub_id, db_id)
        if str(sub_id) == "200" and str(db_id) == "60":
            if result[0] == 200:
                self.events.append("database_200_" + self.eviction)
            elif result[0] == 404 and not self.ess_sub_gone:
                self.events.append("database_404_parent_present")
        return result

    def replace_owned(self, resource, name, uid, changes):
        super().replace_owned(resource, name, uid, changes)
        if resource == qa_live.ESS_DATABASE and self.eviction == "allkeys-lru":
            self.events.append("update_eviction")

    def delete_owned(self, resource, name, uid, namespace=None):
        super().delete_owned(resource, name, uid, namespace)
        if resource == qa_live.ESS_DATABASE:
            self.events.append("delete_database")
        elif resource == qa_live.ESS_SUBSCRIPTION:
            self.events.append("delete_subscription")


class CrossplaneFakeAdapter:
    """Execute the actual harness against fakes and return unverified claims."""

    def __init__(self, world_factory=TracingWorld):
        self.world_factory = world_factory

    def execute(self, scenario):
        if scenario.id != "essentials-database-eviction-update" or scenario.steps != EXPECTED_STEPS:
            return self._result(scenario, "unsupported", (), "not_started")
        world = self.world_factory()
        deps = test_qa_live.deps_for(world)
        try:
            # Guard against a future test fixture accidentally opening a network socket.
            with patch("socket.socket", side_effect=AssertionError("network forbidden")):
                qa_live.strict_essentials(deps)
        except Exception:
            return self._result(scenario, "fail", (), "unknown", bool(world.cloud_deletes))
        # The harness succeeded, but each simulated observation must also occur
        # in order; fake admission is represented by an empty initial inventory,
        # not by any claim of a real account lock.
        wanted = (
            "inventory_empty", "create_subscription", "parent_200", "create_database",
            "database_200_volatile-lru", "update_eviction", "database_200_allkeys-lru",
            "delete_database", "database_404_parent_present", "delete_subscription",
            "parent_404", "inventory_empty",
        )
        iterator = iter(world.events)
        if any(not any(actual == expected for actual in iterator) for expected in wanted) or world.cloud_deletes:
            return self._result(scenario, "fail", (), "unknown", bool(world.cloud_deletes))
        receipts = tuple(StepReceipt(step, "passed", FIXTURE_URL) for step in scenario.steps)
        return self._result(scenario, "pass", receipts, "clean")

    @staticmethod
    def _result(scenario, outcome, steps, cleanup, direct_recovery=False):
        return Evidence("crossplane", scenario.id, "simulated", FIXTURE_REVISION, "", FIXTURE_URL, FIXTURE_URL, steps, outcome, cleanup, direct_recovery)

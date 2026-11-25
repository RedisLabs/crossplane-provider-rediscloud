import unittest
from dataclasses import replace
from pathlib import Path

from contract import classify_claim, load_scenarios
from crossplane_fake_adapter import CrossplaneFakeAdapter, TracingWorld


class CrossplaneFakeAdapterTest(unittest.TestCase):
    def setUp(self):
        self.scenarios = load_scenarios(Path(__file__).with_name("scenarios.json"))
        self.update = self.scenarios["essentials-database-eviction-update"]

    def test_actual_harness_executes_with_fake_world_and_returns_simulated_claim(self):
        result = CrossplaneFakeAdapter().execute(self.update)
        self.assertEqual(result.mode, "simulated")
        self.assertEqual(classify_claim(self.update, result), "simulated_claim_requires_verification")

    def test_existing_subscription_stops_harness_and_never_claims_pass(self):
        world = TracingWorld()
        world.fixed.append({"id": "300", "name": "foreign", "status": "active"})
        result = CrossplaneFakeAdapter(lambda: world).execute(self.update)
        self.assertEqual(result.outcome, "fail")
        self.assertEqual(result.cleanup, "unknown")
        self.assertEqual(world.deleted, [])
        self.assertEqual(world.cloud_deletes, [])

    def test_missing_update_receipt_is_not_a_pass(self):
        class NoUpdateTrace(TracingWorld):
            def replace_owned(self, resource, name, uid, changes):
                super().replace_owned(resource, name, uid, changes)
                self.events.remove("update_eviction")

        result = CrossplaneFakeAdapter(NoUpdateTrace).execute(self.update)
        self.assertEqual(result.outcome, "fail")
        self.assertEqual(result.cleanup, "unknown")

    def test_denied_parent_read_cannot_become_a_pass(self):
        class DeniedParent(TracingWorld):
            def get_fixed_subscription(self, sub_id):
                return 403, None

        result = CrossplaneFakeAdapter(DeniedParent).execute(self.update)
        self.assertEqual(result.outcome, "fail")
        self.assertEqual(result.cleanup, "unknown")

    def test_unsupported_or_changed_scenario_never_runs_harness(self):
        def forbidden():
            self.fail("unsupported case constructed a World")

        adapter = CrossplaneFakeAdapter(forbidden)
        for scenario in (self.scenarios["essentials-database-lifecycle"], replace(self.update, steps=self.update.steps[:-1])):
            result = adapter.execute(scenario)
            self.assertEqual(result.outcome, "unsupported")
            self.assertEqual(result.cleanup, "not_started")


if __name__ == "__main__":
    unittest.main()

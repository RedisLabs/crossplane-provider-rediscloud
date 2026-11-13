"""Hermetic failure-path tests; no Kubernetes or Cloud requests."""
import copy
import json
from types import SimpleNamespace
import unittest
from unittest.mock import Mock

import evidence_run as e
import qa_live as q

NAME = 'xp-red225344-gcp-123-1'


def cr(identifier='42', status='False'):
    return {'kind': 'Subscription', 'metadata': {'name': NAME},
            'spec': {'password': 'secret-sentinel'},
            'status': {'atProvider': {'id': identifier, 'password': 'secret-sentinel'},
                       'conditions': [{'type': 'Ready', 'status': status,
                                       'reason': 'Creating', 'message': 'secret-sentinel'}]}}


class EvidenceTests(unittest.TestCase):
    def setUp(self):
        self.files = {}
        self.write = lambda name, data: self.files.__setitem__(name, copy.deepcopy(data))

    def test_id_persisted_before_readiness_and_failure_capture_sanitized(self):
        state = q.State(NAME)
        obj = cr()
        e.remember_id(q, state, q.SUBSCRIPTION, NAME, obj, self.write)
        self.assertEqual('42', self.files['state.json']['subscription_id'])
        cluster = SimpleNamespace(get=Mock(return_value=obj))
        e.capture_failure(q, cluster, NAME, self.write)
        artifact = json.dumps(self.files)
        self.assertNotIn('secret-sentinel', artifact)
        self.assertIn('Creating', artifact)
        self.assertIn('False', artifact)

    def test_discovery_recovers_id_after_early_error(self):
        state = q.State(NAME)
        e.discover_subscription(state, NAME, [
            {'name': 'operator-unrelated', 'id': 500},
            {'name': NAME, 'id': 42, 'status': 'error'}], self.write)
        self.assertEqual('42', self.files['state.json']['subscription_id'])

    def test_duplicate_discovery_fails_and_persists_conflict(self):
        state = q.State(NAME)
        with self.assertRaises(RuntimeError):
            e.discover_subscription(state, NAME, [
                {'name': NAME, 'id': 42}, {'name': NAME, 'id': 43}], self.write)
        self.assertTrue(self.files['state.json']['id_conflict'])
        self.assertIsNone(state.subscription_id)

    def test_changed_discovered_id_preserves_original_and_fails(self):
        state = q.State(NAME)
        state.subscription_id = '42'
        with self.assertRaises(RuntimeError):
            e.discover_subscription(state, NAME, [{'name': NAME, 'id': 43}], self.write)
        self.assertEqual('42', state.subscription_id)
        self.assertTrue(state.id_conflict)

    def test_failure_snapshot_continues_after_read_error(self):
        cluster = SimpleNamespace(get=Mock(side_effect=[
            RuntimeError('secret-sentinel'), None, cr(), None, None]))
        e.capture_failure(q, cluster, NAME, self.write)
        rows = self.files['failure-kubernetes.json']
        self.assertEqual(5, len(rows))
        self.assertTrue(rows[0]['read_failed'])
        self.assertEqual('42', rows[2]['status']['id'])
        self.assertNotIn('secret-sentinel', json.dumps(rows))

    def test_missing_ids_are_not_invented(self):
        state = q.State(NAME)
        e.remember_id(q, state, q.SUBSCRIPTION, NAME, cr(None), self.write)
        self.assertIsNone(state.subscription_id)

    def test_foreign_resource_rejected(self):
        obj = cr()
        obj['metadata']['name'] = 'operator-unrelated'
        with self.assertRaises(RuntimeError):
            e.remember_id(q, q.State(NAME), q.SUBSCRIPTION, NAME, obj, self.write)

    def test_database_subscription_mismatch_rejected(self):
        state = q.State(NAME)
        state.subscription_id = '42'
        with self.assertRaises(RuntimeError):
            e.remember_id(q, state, q.DATABASE, NAME, cr('43/99'), self.write)
        self.assertIsNone(state.database_id)

    def test_cloud_error_retains_discovered_id(self):
        state = q.State(NAME)
        def get(kind, name):
            obj = cr()
            e.remember_id(q, state, kind, name, obj, self.write)
            return obj
        deps = SimpleNamespace(cloud=SimpleNamespace(list_subscriptions=lambda: [
            {'name': NAME, 'id': 42, 'status': 'error'}]),
            cluster=SimpleNamespace(get=get), log=lambda _: None)
        with self.assertRaises(q.SuiteFailure):
            q.subscription_is_active(deps, NAME, {})
        self.assertEqual('42', self.files['state.json']['subscription_id'])

    def test_timeout_retains_discovered_id(self):
        state = q.State(NAME)
        tick = iter([0, 0, 2])
        deps = SimpleNamespace(now=lambda: next(tick), sleep=lambda _: None,
                               log=lambda _: None, poll=1)
        def pending():
            e.remember_id(q, state, q.SUBSCRIPTION, NAME, cr(), self.write)
            return False
        self.assertFalse(q.wait_until(deps, 'subscription', 1, pending))
        self.assertEqual('42', self.files['state.json']['subscription_id'])

    def cleanup(self, stuck=None, error=None, missing_id=False, http=404, conflict=False, lookup_error=False):
        deleted = []
        def delete(kind, name):
            self.assertEqual(NAME, name)
            deleted.append(kind)
            if kind == error:
                raise RuntimeError('secret-sentinel')
        cluster = SimpleNamespace(delete=delete, get=lambda k, n: cr() if k == stuck else None)
        fakeq = SimpleNamespace(DATABASE=q.DATABASE, SUBSCRIPTION=q.SUBSCRIPTION,
            ACL=q.ACL, wait_until=lambda d, label, timeout, ready: ready(),
            delete_lookups=Mock(side_effect=RuntimeError("secret-sentinel") if lookup_error else None))
        api = SimpleNamespace(list_subscriptions=lambda: [], list_acl_rules=lambda: [],
            get_subscription=lambda _: (http, {}), get_database=lambda *_: (http, {}))
        state = {'run_name': NAME, 'subscription_id': None if missing_id else '42',
                 'database_id': '99', 'id_conflict': conflict}
        try:
            e.cleanup_resources(fakeq, SimpleNamespace(), cluster, api, NAME, state, self.write)
        except RuntimeError:
            pass
        return deleted, fakeq

    def test_subscription_timeout_does_not_skip_acl_or_pass(self):
        deleted, fakeq = self.cleanup(stuck=q.SUBSCRIPTION)
        self.assertEqual([q.DATABASE, q.SUBSCRIPTION, q.ACL], deleted)
        self.assertEqual('FAILED', self.files['06-cleanup.json']['result'])
        fakeq.delete_lookups.assert_not_called()

    def test_database_timeout_skips_parent_but_attempts_acl(self):
        deleted, _ = self.cleanup(stuck=q.DATABASE)
        self.assertEqual([q.DATABASE, q.ACL], deleted)
        self.assertEqual('FAILED', self.files['06-cleanup.json']['result'])

    def test_delete_exception_is_sanitized_and_acl_attempted(self):
        deleted, _ = self.cleanup(error=q.SUBSCRIPTION)
        self.assertIn(q.ACL, deleted)
        self.assertNotIn('secret-sentinel', json.dumps(self.files))
        self.assertEqual('FAILED', self.files['06-cleanup.json']['result'])

    def test_conflicting_id_cannot_pass_even_with_404(self):
        self.cleanup(conflict=True)
        self.assertEqual('FAILED', self.files['06-cleanup.json']['result'])

    def test_lookup_failure_rewrites_result_to_failed(self):
        self.cleanup(lookup_error=True)
        self.assertEqual('FAILED', self.files['06-cleanup.json']['result'])
        self.assertNotIn('secret-sentinel', json.dumps(self.files))

    def test_missing_id_cannot_pass(self):
        self.cleanup(missing_id=True)
        self.assertEqual('FAILED', self.files['06-cleanup.json']['result'])

    def test_accepted_response_is_not_deletion(self):
        self.cleanup(http=202)
        self.assertEqual('FAILED', self.files['06-cleanup.json']['result'])

    def test_confirmed_absence_passes_and_removes_lookups(self):
        _, fakeq = self.cleanup()
        self.assertEqual('PASSED', self.files['06-cleanup.json']['result'])
        fakeq.delete_lookups.assert_called_once()

    def test_foreign_state_stops_before_delete(self):
        cluster = Mock()
        with self.assertRaises(RuntimeError):
            e.cleanup_resources(q, None, cluster, None, NAME,
                                {'run_name': 'other'}, self.write)
        cluster.delete.assert_not_called()


if __name__ == '__main__':
    unittest.main()

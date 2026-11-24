import threading
import unittest
from types import SimpleNamespace

import qa_delete_fence as fence


class AtomicStore:
    def __init__(self):
        self.refs = {}
        self.lock = threading.Lock()

    def create(self, ref, sha):
        with self.lock:
            if ref in self.refs:
                return False
            self.refs[ref] = sha
            return True

    def read(self, ref):
        return self.refs.get(ref)


class DeleteFenceTest(unittest.TestCase):
    def setUp(self):
        self.key = b"k" * 32
        self.origin = "a" * 64
        self.account = "b" * 64
        self.ref = fence.fence_ref(self.key, self.origin, self.account, "123", "456", "database")
        self.sha = "c" * 40

    def test_stable_opaque_identity_and_parent_scope(self):
        self.assertEqual(self.ref, fence.fence_ref(self.key, self.origin, self.account, "123", "456", "database"))
        self.assertNotIn("123", self.ref)
        self.assertEqual(fence.fence_ref(self.key, self.origin, self.account, "123", "456", "subscription"),
                         fence.fence_ref(self.key, self.origin, self.account, "123", "789", "subscription"))
        self.assertNotEqual(self.ref, fence.fence_ref(self.key, self.origin, self.account, "123", "789", "database"))

    def test_crash_after_create_never_authorizes_retry(self):
        store = AtomicStore()
        fence.reserve_once(store, self.ref, self.sha)
        # The process may die before, during, or after sending DELETE. Every
        # subsequent attempt sees the marker and refuses regardless of run ID.
        for _ in range(3):
            with self.assertRaises(fence.FenceUncertain):
                fence.reserve_once(store, self.ref, self.sha)

    def test_lost_create_response_fails_even_if_store_committed(self):
        store = AtomicStore()
        original = store.create
        def lost(ref, sha):
            original(ref, sha)
            raise TimeoutError()
        store.create = lost
        with self.assertRaises(fence.FenceUncertain):
            fence.reserve_once(store, self.ref, self.sha)
        store.create = original
        with self.assertRaises(fence.FenceUncertain):
            fence.reserve_once(store, self.ref, self.sha)

    def test_competing_attempts_have_one_winner(self):
        store = AtomicStore()
        outcomes = []
        def try_reserve():
            try:
                fence.reserve_once(store, self.ref, self.sha)
                outcomes.append("reserved")
            except fence.FenceUncertain:
                outcomes.append("refused")
        threads = [threading.Thread(target=try_reserve) for _ in range(12)]
        for thread in threads: thread.start()
        for thread in threads: thread.join()
        self.assertEqual(outcomes.count("reserved"), 1)
        self.assertEqual(outcomes.count("refused"), 11)

    def test_invalid_or_unverifiable_marker_never_authorizes(self):
        for returned in (None, "", "d" * 40):
            store = AtomicStore()
            store.read = lambda ref: returned
            with self.subTest(returned=returned), self.assertRaises(fence.FenceUncertain):
                fence.reserve_once(store, self.ref, self.sha)
        with self.assertRaises(fence.FenceUncertain):
            fence.fence_ref(self.key, self.origin, self.account, "0", "456", "database")

    def test_github_adapter_never_retries_an_uncertain_create(self):
        calls = []
        def failed(*args, **kwargs):
            calls.append(args)
            return SimpleNamespace(returncode=1, stdout="", stderr="private error")
        store = fence.GitHubRefStore("RedisLabs/crossplane-provider-rediscloud", run=failed)
        with self.assertRaises(fence.FenceUncertain):
            fence.reserve_once(store, self.ref, self.sha)
        self.assertEqual(len(calls), 1)

    def test_github_adapter_accepts_only_matching_commit_readback(self):
        def response(args, **kwargs):
            if "POST" in args:
                return SimpleNamespace(returncode=0, stdout='{"ref":"%s","object":{"sha":"%s"}}' % (self.ref, self.sha))
            return SimpleNamespace(returncode=0, stdout='{"ref":"%s","object":{"sha":"%s","type":"tag"}}' % (self.ref, self.sha))
        with self.assertRaises(fence.FenceUncertain):
            fence.reserve_once(fence.GitHubRefStore("RedisLabs/crossplane-provider-rediscloud", run=response), self.ref, self.sha)

    def test_read_only_inspection_never_authorizes_or_creates(self):
        store = AtomicStore()
        self.assertEqual(fence.inspect_fence(store, self.ref, self.sha), "absent-unverified")
        self.assertEqual(store.refs, {})
        fence.reserve_once(store, self.ref, self.sha)
        self.assertEqual(fence.inspect_fence(store, self.ref, self.sha), "reserved-manual-review")
        self.assertEqual(fence.inspect_fence(store, self.ref, "d" * 40), "unknown")
        store.read = lambda ref: (_ for _ in ()).throw(TimeoutError())
        self.assertEqual(fence.inspect_fence(store, self.ref, self.sha), "unknown")


if __name__ == "__main__":
    unittest.main()

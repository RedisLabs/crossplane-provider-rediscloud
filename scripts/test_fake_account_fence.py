"""Hosted-only synthetic protocol rehearsal; never use with real Cloud credentials."""
import json
import os
import threading
import unittest
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from uuid import uuid4


class Denied(Exception):
    pass


class FakeCloud:
    def __init__(self):
        self.resources = {'standing'}
        self.complete = True
        self.calls = []
        self.pending = set()
        self.lock = threading.Lock()

    def apply(self, body):
        with self.lock:
            self.calls.append(body['id'])
            if body['kind'] == 'create':
                self.resources.add(body['resource'])
            elif body['kind'] == 'delete':
                self.resources.discard(body['resource'])
            elif body['kind'] == 'pending':
                self.pending.add(body['id'])
            else:
                raise ValueError('bad fixture operation')

    def snapshot(self):
        with self.lock:
            return {'resources': sorted(self.resources), 'pending': sorted(self.pending),
                    'complete': self.complete}


class Broker:
    """In-memory model only: no auth, durable store, or real Cloud support."""
    def __init__(self, endpoint):
        self.endpoint = endpoint
        self.lock = threading.Lock()
        self.owner = None
        self.generation = 0
        self.inflight = set()
        self.seen = set()
        self.uncertain = set()
        self.baseline = {'standing'}
        self.intended = set()
        self.delete_reserved = set()
        self.busy_resources = set()

    def acquire(self, run):
        with self.lock:
            if self.owner is not None:
                raise Denied()
            self.generation += 1
            self.owner = (run, self.generation, uuid4().hex)
            self.intended = set()
            self.delete_reserved = set()
            return self.owner

    def _check(self, owner):
        if owner is None or owner != self.owner:
            raise Denied()

    def mutate(self, owner, request_id, kind, resource, ready=None, proceed=None, lose_reply=False):
        with self.lock:
            self._check(owner)
            if request_id in self.seen:
                raise Denied()
            # Fixed fixture identity only; this is not proof of Cloud ownership.
            if resource != 'run-db' or (kind == 'delete' and resource not in self.intended):
                raise Denied()
            if resource in self.busy_resources:
                raise Denied()
            if kind == 'delete':
                if resource in self.delete_reserved or self.uncertain:
                    raise Denied()
                self.delete_reserved.add(resource)
            if kind == 'create':
                if resource in self.intended:
                    raise Denied()
                self.intended.add(resource)
            self.seen.add(request_id)
            self.inflight.add(request_id)
            self.busy_resources.add(resource)
        if ready is not None:
            ready.set()
            proceed.wait(10)
        try:
            payload = json.dumps({'id': request_id, 'kind': kind, 'resource': resource}).encode()
            req = urllib.request.Request(self.endpoint + '/mutate', data=payload,
                                         headers={'Content-Type': 'application/json'})
            with urllib.request.urlopen(req, timeout=5) as response:
                if response.status != 200:
                    raise Denied()
            if lose_reply:
                raise OSError('simulated unknown response')
        except Exception:
            with self.lock:
                self.uncertain.add(request_id)
            raise
        finally:
            with self.lock:
                self.inflight.remove(request_id)
                self.busy_resources.remove(resource)

    def release(self, owner):
        with self.lock:
            self._check(owner)
            if self.inflight or self.uncertain:
                raise Denied()
            with urllib.request.urlopen(self.endpoint + '/inventory', timeout=5) as response:
                state = json.load(response)
            if (state.get('complete') is not True or state.get('pending') != []
                    or set(state.get('resources', [])) != self.baseline):
                raise Denied()
            self.owner = None


@unittest.skipUnless(os.getenv('GITHUB_ACTIONS') == 'true', 'hosted Actions only')
class AccountFenceRehearsal(unittest.TestCase):
    def setUp(self):
        self.cloud = FakeCloud()
        cloud = self.cloud

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *_args):
                pass

            def do_POST(self):
                if self.path != '/mutate':
                    self.send_error(404)
                    return
                body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                cloud.apply(body)
                self.send_response(200)
                self.end_headers()

            def do_GET(self):
                if self.path != '/inventory':
                    self.send_error(404)
                    return
                data = json.dumps(cloud.snapshot()).encode()
                self.send_response(200)
                self.end_headers()
                self.wfile.write(data)

        self.server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.thread.start()
        self.broker = Broker('http://127.0.0.1:' + str(self.server.server_port))

    def tearDown(self):
        self.server.shutdown()
        self.server.server_close()
        self.thread.join()

    def test_concurrent_clients_stale_generation_and_replay(self):
        barrier = threading.Barrier(3)
        outcomes = []
        def acquire(name):
            barrier.wait()
            try:
                outcomes.append(self.broker.acquire(name))
            except Denied:
                outcomes.append(None)
        clients = [threading.Thread(target=acquire, args=(name,)) for name in ('operator', 'crossplane')]
        for client in clients:
            client.start()
        barrier.wait()
        for client in clients:
            client.join()
        winners = [x for x in outcomes if x is not None]
        self.assertEqual(len(winners), 1)
        old = winners[0]
        self.broker.mutate(old, 'create', 'create', 'run-db')
        with self.assertRaises(Denied):
            self.broker.mutate(old, 'create', 'create', 'run-db')
        with self.assertRaises(Denied):
            self.broker.release(old)
        self.broker.mutate(old, 'delete', 'delete', 'run-db')
        with self.assertRaises(Denied):
            self.broker.mutate(old, 'delete-again', 'delete', 'run-db')
        self.broker.release(old)
        new = self.broker.acquire('next-attempt')
        self.assertGreater(new[1], old[1])
        with self.assertRaises(Denied):
            self.broker.mutate(old, 'stale', 'create', 'foreign')
        with self.assertRaises(Denied):
            self.broker.release(old)
        self.assertEqual(self.cloud.calls, ['create', 'delete'])

    def test_inflight_unknown_and_recovery_block_release(self):
        owner = self.broker.acquire('crossplane-attempt-1')
        entered, proceed = threading.Event(), threading.Event()
        worker = threading.Thread(target=self.broker.mutate,
                                  args=(owner, 'create', 'create', 'run-db', entered, proceed))
        worker.start()
        self.assertTrue(entered.wait(5))
        with self.assertRaises(Denied):
            self.broker.release(owner)
        with self.assertRaises(Denied):
            self.broker.acquire('operator')
        with self.assertRaises(Denied):
            self.broker.mutate(owner, 'racing-delete', 'delete', 'run-db')
        proceed.set()
        worker.join(10)
        self.assertFalse(worker.is_alive())
        self.assertEqual(self.cloud.calls, ['create'])
        self.assertIn('run-db', self.cloud.snapshot()['resources'])
        with self.assertRaises(Denied):
            self.broker.release(owner)
        # Recovery remains under original ownership and removes only its own item.
        self.broker.mutate(owner, 'cleanup', 'delete', 'run-db')
        self.cloud.complete = False
        with self.assertRaises(Denied):
            self.broker.release(owner)
        self.cloud.complete = True
        self.broker.release(owner)
        self.assertEqual(self.cloud.snapshot()['resources'], ['standing'])

    def test_unknown_outcome_and_async_task_permanently_block_model(self):
        owner = self.broker.acquire('run')
        with self.assertRaises(OSError):
            self.broker.mutate(owner, 'unknown', 'create', 'run-db', lose_reply=True)
        with self.assertRaises(Denied):
            self.broker.mutate(owner, 'unknown', 'create', 'run-db')
        with self.assertRaises(Denied):
            self.broker.mutate(owner, 'cleanup', 'delete', 'run-db')
        with self.assertRaises(Denied):
            self.broker.release(owner)
        with self.assertRaises(Denied):
            self.broker.acquire('other')
        self.assertEqual(self.cloud.calls, ['unknown'])

    def test_lost_delete_response_cannot_be_replayed_under_new_request_id(self):
        owner = self.broker.acquire('run')
        self.broker.mutate(owner, 'create', 'create', 'run-db')
        with self.assertRaises(OSError):
            self.broker.mutate(owner, 'delete-1', 'delete', 'run-db', lose_reply=True)
        with self.assertRaises(Denied):
            self.broker.mutate(owner, 'delete-2', 'delete', 'run-db')
        with self.assertRaises(Denied):
            self.broker.release(owner)
        self.assertEqual(self.cloud.calls, ['create', 'delete-1'])

    def test_pending_task_and_foreign_resource_refuse_release(self):
        owner = self.broker.acquire('run')
        self.broker.mutate(owner, 'async', 'pending', 'run-db')
        with self.assertRaises(Denied):
            self.broker.release(owner)
        self.cloud.pending.clear()
        self.cloud.resources.add('foreign')
        with self.assertRaises(Denied):
            self.broker.release(owner)
        self.cloud.resources.remove('foreign')
        self.broker.release(owner)

    def test_owner_cannot_delete_standing_or_unintended_resource(self):
        owner = self.broker.acquire('run')
        for resource in ('standing', 'foreign', 'run-db'):
            with self.assertRaises(Denied):
                self.broker.mutate(owner, 'delete-' + resource, 'delete', resource)
        self.assertEqual(self.cloud.calls, [])
        self.assertEqual(self.cloud.snapshot()['resources'], ['standing'])

    def test_direct_fake_cloud_bypasses_model_broker(self):
        self.broker.acquire('run')
        payload = json.dumps({'id': 'direct', 'kind': 'create', 'resource': 'foreign'}).encode()
        request = urllib.request.Request(self.broker.endpoint + '/mutate', data=payload,
                                         headers={'Content-Type': 'application/json'})
        with urllib.request.urlopen(request, timeout=5) as response:
            self.assertEqual(response.status, 200)
        self.assertIn('foreign', self.cloud.snapshot()['resources'])


if __name__ == '__main__':
    unittest.main()

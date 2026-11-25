import http.client
import os
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

from fake_proxy import FAKE_KEY, FAKE_SECRET, FakeProxy


class Upstream(BaseHTTPRequestHandler):
    calls = []
    redirect = False

    def log_message(self, *_args):
        pass

    def do_GET(self):
        self.calls.append((self.command, self.path))
        if self.redirect:
            self.send_response(302)
            self.send_header("Location", "https://example.invalid/must-not-follow")
            self.end_headers()
            return
        self.send_response(200)
        self.end_headers()
        self.wfile.write(b'{"fixture":true}')


class TestFakeProxy(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if os.environ.get("GITHUB_ACTIONS") != "true":
            raise unittest.SkipTest("fixture execution requires GitHub Actions")
        cls.upstream = ThreadingHTTPServer(("127.0.0.1", 0), Upstream)
        cls.proxy = FakeProxy(cls.upstream.server_port)
        for server in (cls.upstream, cls.proxy):
            threading.Thread(target=server.serve_forever, daemon=True).start()

    @classmethod
    def tearDownClass(cls):
        for server in (cls.proxy, cls.upstream):
            server.shutdown()
            server.server_close()

    def setUp(self):
        Upstream.calls = []
        Upstream.redirect = False

    def request(self, method, path, headers=None):
        conn = http.client.HTTPConnection("127.0.0.1", self.proxy.server_port, timeout=5)
        conn.request(method, path, headers=headers or {"X-Api-Key": FAKE_KEY, "X-Api-Secret-Key": FAKE_SECRET})
        response = conn.getresponse()
        result = (response.status, dict(response.getheaders()), response.read())
        conn.close()
        return result

    def test_only_exact_get_routes_are_forwarded(self):
        for path in ("/v1/regions", "/v1/payment-methods"):
            self.assertEqual(self.request("GET", path)[0], 200)
        self.assertEqual(Upstream.calls, [("GET", "/v1/regions"), ("GET", "/v1/payment-methods")])

    def test_mutations_and_ambiguous_paths_never_reach_upstream(self):
        for method, path in (("POST", "/v1/regions"), ("DELETE", "/v1/payment-methods"), ("GET", "/v1/regions?foo=1"), ("GET", "/v1/regions/"), ("GET", "/v1/%72egions"), ("GET", "/v1/../v1/regions"), ("GET", "http://example.invalid/v1/regions")):
            with self.subTest(method=method, path=path):
                self.assertEqual(self.request(method, path)[0], 403)
        self.assertEqual(Upstream.calls, [])

    def test_redirect_is_not_forwarded_or_followed(self):
        Upstream.redirect = True
        status, headers, body = self.request("GET", "/v1/regions")
        self.assertEqual((status, body), (502, b""))
        self.assertNotIn("Location", headers)
        self.assertEqual(Upstream.calls, [("GET", "/v1/regions")])

    def test_real_or_missing_credentials_rejected_without_reflection(self):
        for headers in ({"X-Api-Key": "not-fixture", "X-Api-Secret-Key": "private-value"}, {"X-Api-Key": FAKE_KEY}):
            status, _, body = self.request("GET", "/v1/payment-methods", headers)
            self.assertEqual((status, body), (403, b""))
        self.assertEqual(Upstream.calls, [])

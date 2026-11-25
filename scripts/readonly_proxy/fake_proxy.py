"""Fake-only, loopback-only route guard. This cannot connect to Redis Cloud."""
from __future__ import annotations

import http.client
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

ALLOWED = frozenset(("/v1/regions", "/v1/payment-methods"))
MAX_BODY = 2 * 1024 * 1024
FAKE_KEY = "fixture-key"
FAKE_SECRET = "fixture-secret"


class FakeProxy(ThreadingHTTPServer):
    def __init__(self, upstream_port: int):
        if type(upstream_port) is not int or not 1 <= upstream_port <= 65535:
            raise ValueError("invalid loopback fixture port")
        self.upstream_port = upstream_port
        super().__init__(("127.0.0.1", 0), Handler)


class Handler(BaseHTTPRequestHandler):
    def log_message(self, *_args):
        pass

    def deny(self, code=403):
        self.send_response(code)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_GET(self):
        if (self.path not in ALLOWED or self.headers.get("Transfer-Encoding") is not None
                or self.headers.get("Content-Length", "0") != "0"
                or self.headers.get_all("X-Api-Key") != [FAKE_KEY]
                or self.headers.get_all("X-Api-Secret-Key") != [FAKE_SECRET]):
            self.deny()
            return
        conn = http.client.HTTPConnection("127.0.0.1", self.server.upstream_port, timeout=3)
        try:
            conn.request("GET", self.path, headers={"X-Api-Key": FAKE_KEY, "X-Api-Secret-Key": FAKE_SECRET})
            response = conn.getresponse()
            if response.status != 200:
                self.deny(502)
                return
            data = response.read(MAX_BODY + 1)
            if len(data) > MAX_BODY:
                self.deny(502)
                return
            self.send_response(200)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(data)))
            self.end_headers()
            self.wfile.write(data)
        except (OSError, http.client.HTTPException):
            self.deny(502)
        finally:
            conn.close()

    def do_POST(self):
        self.deny()

    do_PUT = do_PATCH = do_DELETE = do_HEAD = do_OPTIONS = do_CONNECT = do_POST

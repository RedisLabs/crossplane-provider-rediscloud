"""Credential-free transport rehearsal; no cluster or Cloud access."""
import json
import os
from pathlib import Path
import tempfile
import threading
import unittest
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from unittest.mock import patch

import qa_live


@unittest.skipUnless(os.environ.get("QA_TEST_KUBECTL"), "pinned kubectl not supplied")
class KubectlUIDTransportTest(unittest.TestCase):
    def test_raw_delete_sends_uid_and_replacement_returns_409(self):
        requests = []
        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass
            def do_DELETE(self):
                if self.headers.get("Transfer-Encoding", "").lower() == "chunked":
                    chunks = []
                    while True:
                        size = int(self.rfile.readline().split(b";", 1)[0].strip(), 16)
                        if size == 0:
                            self.rfile.readline()
                            break
                        chunks.append(self.rfile.read(size))
                        self.rfile.read(2)
                    raw = b"".join(chunks)
                else:
                    raw = self.rfile.read(int(self.headers.get("Content-Length", "0")))
                body = json.loads(raw)
                requests.append((self.path, body))
                uid = body.get("preconditions", {}).get("uid")
                code = 200 if uid == "current-uid" else 409
                result = {"kind": "Status", "apiVersion": "v1",
                          "status": "Success" if code == 200 else "Failure",
                          "reason": "Conflict" if code == 409 else "",
                          "message": "UID precondition failed" if code == 409 else "deleted",
                          "code": code}
                payload = json.dumps(result).encode()
                self.send_response(code)
                self.send_header("Content-Type", "application/json")
                self.send_header("Content-Length", str(len(payload)))
                self.end_headers()
                self.wfile.write(payload)

        server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        try:
            with tempfile.TemporaryDirectory() as tmp:
                config = {"apiVersion": "v1", "kind": "Config",
                          "clusters": [{"name": "local", "cluster": {"server": f"http://127.0.0.1:{server.server_port}"}}],
                          "users": [{"name": "local", "user": {}}],
                          "contexts": [{"name": qa_live.ALLOWED_CONTEXT, "context": {"cluster": "local", "user": "local"}}],
                          "current-context": qa_live.ALLOWED_CONTEXT}
                kubeconfig = Path(tmp) / "config.json"
                kubeconfig.write_text(json.dumps(config))
                deps = qa_live.Deps(None, None, "123")
                cluster = qa_live.KubectlCluster(os.environ["QA_TEST_KUBECTL"], qa_live.ALLOWED_CONTEXT, {}, deps)
                with patch.dict(os.environ, {"KUBECONFIG": str(kubeconfig)}):
                    with self.assertRaises(qa_live.SuiteFailure):
                        cluster.delete_owned(qa_live.ESS_DATABASE, "xp-ci-123", "old-uid")
                    resources = [
                        (qa_live.ESS_DATABASE, "/apis/essentials.redis.io/v1beta1/databases/"),
                        (qa_live.ESS_SUBSCRIPTION, "/apis/essentials.redis.io/v1beta1/subscriptions/"),
                        (qa_live.DATABASE, "/apis/subscription.redis.io/v1beta1/databases/"),
                        (qa_live.SUBSCRIPTION, "/apis/rediscloud.redis.io/v1beta1/subscriptions/"),
                        (qa_live.ACL, "/apis/acl.redis.io/v1beta1/rules/"),
                        (qa_live.ROLE, "/apis/acl.redis.io/v1beta1/roles/"),
                        (qa_live.USER, "/apis/acl.redis.io/v1beta1/users/"),
                        (qa_live.PAYMENT, "/apis/lookup.redis.io/v1alpha1/paymentmethods/"),
                        (qa_live.PLAN, "/apis/lookup.redis.io/v1alpha1/essentialsplans/"),
                        (qa_live.REGIONS, "/apis/lookup.redis.io/v1alpha1/regions/"),
                        (qa_live.PROVIDER_CONFIG, "/apis/rediscloud.redis.io/v1beta1/providerconfigs/"),
                    ]
                    for resource, _ in resources:
                        cluster.delete_owned(resource, "xp-ci-123", "current-uid")
                    cluster.delete_owned("secret", "xp-ci-123", "current-uid", namespace="upbound-system")
        finally:
            server.shutdown()
            server.server_close()
            thread.join()
        self.assertEqual(len(requests), len(resources) + 2)
        self.assertEqual(requests[0][0], "/apis/essentials.redis.io/v1beta1/databases/xp-ci-123")
        self.assertEqual(requests[0][1]["preconditions"], {"uid": "old-uid"})
        for (path, body), (_, prefix) in zip(requests[1:-1], resources):
            self.assertEqual(path, prefix + "xp-ci-123")
            self.assertEqual(body["preconditions"], {"uid": "current-uid"})
        self.assertEqual(requests[-1][0], "/api/v1/namespaces/upbound-system/secrets/xp-ci-123")


if __name__ == "__main__":
    unittest.main()

import hashlib
import json
import subprocess
import tempfile
import unittest
from pathlib import Path

from qa_origin_ciphertext import build_payload, encrypt, selected_digest


class CiphertextTests(unittest.TestCase):
    def test_select_unique_and_reject_bad_allowlists(self):
        digest = hashlib.sha256(b"https://qa.example.test").hexdigest()
        raw = json.dumps({"url": "https://qa.example.test/v1"})
        self.assertEqual(selected_digest(raw, digest), digest)
        for bad in ("", "f" * 64, digest + "," + digest, "bad", ",".join([digest] * 17)):
            with self.subTest(bad=bad), self.assertRaises(ValueError):
                selected_digest(raw, bad)

    def test_ephemeral_key_round_trip_and_nondeterminism(self):
        with tempfile.TemporaryDirectory() as directory:
            private, public = Path(directory) / "key", Path(directory) / "pub"
            subprocess.run(["openssl", "genpkey", "-quiet", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:3072", "-out", str(private)], check=True, stderr=subprocess.DEVNULL)
            subprocess.run(["openssl", "pkey", "-in", str(private), "-pubout", "-out", str(public)], check=True, stderr=subprocess.DEVNULL)
            payload = b'{"purpose":"dummy-fixture"}'
            first, second = encrypt(payload, public), encrypt(payload, public)
            self.assertNotEqual(first, second)
            plaintext = subprocess.run(["openssl", "pkeyutl", "-decrypt", "-inkey", str(private), "-pkeyopt", "rsa_padding_mode:oaep", "-pkeyopt", "rsa_oaep_md:sha256", "-pkeyopt", "rsa_mgf1_md:sha256"], input=first, stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=True).stdout
            self.assertEqual(plaintext, payload)

    def test_context_bound_and_invalid_context_rejected(self):
        env = {"GITHUB_RUN_ID": "1", "GITHUB_RUN_ATTEMPT": "2", "GITHUB_SHA": "a" * 40,
               "GITHUB_EVENT_NAME": "workflow_dispatch", "GITHUB_ACTOR": "shay-berman",
               "GITHUB_REPOSITORY": "RedisLabs/crossplane-provider-rediscloud",
               "GITHUB_REF": "refs/heads/codex/qa-origin-ciphertext"}
        payload = json.loads(build_payload("b" * 64, env))
        self.assertEqual((payload["run"], payload["attempt"], payload["sha"]), ("1", "2", "a" * 40))
        for key, value in (("GITHUB_RUN_ATTEMPT", "0"), ("GITHUB_RUN_ID", ""),
                           ("GITHUB_SHA", "bad"), ("GITHUB_EVENT_NAME", "push"),
                           ("GITHUB_ACTOR", "other"), ("GITHUB_REF", "refs/heads/other")):
            with self.subTest(key=key), self.assertRaises(ValueError):
                build_payload("b" * 64, {**env, key: value})


if __name__ == "__main__":
    unittest.main()

import copy
import hashlib
import io
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch
import zipfile

from qa_origin_ciphertext import encrypt
import qa_origin_reader as reader


class OriginReaderTests(unittest.TestCase):
    def setUp(self):
        self.run_id, self.attempt, self.sha, self.artifact_id = "123", "2", "a" * 40, "456"
        self.run = {"id": 123, "run_attempt": 2, "head_sha": self.sha, "path": ".github/workflows/ci.yml",
                    "head_branch": reader.BRANCH, "event": "workflow_dispatch", "actor": {"login": "shay-berman"},
                    "triggering_actor": {"login": "shay-berman"}, "repository": {"full_name": reader.REPO},
                    "head_repository": {"full_name": reader.REPO}, "status": "completed", "conclusion": "success"}
        self.jobs = {"total_count": 1, "jobs": [{"name": "qa-origin-ciphertext", "status": "completed", "conclusion": "success", "run_id": 123, "run_attempt": 2, "head_sha": self.sha}]}
        self.artifact = {"id": 456, "name": "qa-origin-ciphertext-123-2", "expired": False,
                         "size_in_bytes": 500, "digest": "sha256:" + "b" * 64,
                         "workflow_run": {"id": 123, "head_sha": self.sha, "head_branch": reader.BRANCH}}
        self.artifacts = {"total_count": 1, "artifacts": [self.artifact]}

    def verify(self, run=None, jobs=None, artifacts=None):
        return reader.verify_metadata(run or self.run, jobs or self.jobs, artifacts or self.artifacts,
                                      self.run_id, self.attempt, self.sha, self.artifact_id)

    def test_metadata_rejects_wrong_context_and_incomplete_inventory(self):
        self.assertEqual(self.verify(), "sha256:" + "b" * 64)
        for key, value in (("path", ".github/workflows/other.yml"), ("head_sha", "c" * 40),
                           ("run_attempt", 1), ("event", "push"), ("actor", {"login": "other"}),
                           ("triggering_actor", {"login": "other"}), ("conclusion", "failure")):
            changed = {**self.run, key: value}
            with self.subTest(key=key), self.assertRaises(ValueError):
                self.verify(run=changed)
        bad_job = {"total_count": 1, "jobs": [{"name": "qa-origin-ciphertext", "status": "completed", "conclusion": "failure"}]}
        with self.assertRaises(ValueError):
            self.verify(jobs=bad_job)
        for changed in ({**self.artifact, "id": 999}, {**self.artifact, "expired": True}, {**self.artifact, "digest": "bad"}):
            with self.assertRaises(ValueError):
                self.verify(artifacts={"total_count": 1, "artifacts": [changed]})
        with self.assertRaises(ValueError):
            self.verify(artifacts={"total_count": 2, "artifacts": [self.artifact, copy.deepcopy(self.artifact)]})
        with self.assertRaises(ValueError):
            self.verify(jobs={"total_count": 2, "jobs": self.jobs["jobs"]})

    def test_archive_and_context_fail_closed(self):
        buf = io.BytesIO()
        with zipfile.ZipFile(buf, "w") as bundle:
            bundle.writestr("qa-origin-ciphertext.bin", b"a" * 384)
        archive = buf.getvalue()
        digest = "sha256:" + hashlib.sha256(archive).hexdigest()
        self.assertEqual(reader.extract_ciphertext(archive, digest), b"a" * 384)
        with self.assertRaises(ValueError):
            reader.extract_ciphertext(archive + b"x", digest)
        payload = {"purpose": "qa-origin-candidate-v1", "run": "123", "attempt": "2", "sha": self.sha, "digest": "d" * 64}
        self.assertEqual(reader.checked_payload(json.dumps(payload), self.run_id, self.attempt, self.sha), "d" * 64)
        for key, value in (("run", "124"), ("attempt", "1"), ("sha", "e" * 40), ("purpose", "other")):
            with self.subTest(key=key), self.assertRaises(ValueError):
                reader.checked_payload(json.dumps({**payload, key: value}), self.run_id, self.attempt, self.sha)
        with self.assertRaises(ValueError):
            reader.checked_payload('{"run":"123","run":"124"}', self.run_id, self.attempt, self.sha)

    def test_decryption_rejects_wrong_key_and_tampering(self):
        with tempfile.TemporaryDirectory() as directory:
            private, public = Path(directory) / "key", Path(directory) / "pub"
            subprocess.run(["openssl", "genpkey", "-quiet", "-algorithm", "RSA", "-pkeyopt", "rsa_keygen_bits:3072", "-out", str(private)], check=True, stderr=subprocess.DEVNULL)
            private.chmod(0o600)
            subprocess.run(["openssl", "pkey", "-in", str(private), "-pubout", "-out", str(public)], check=True, stderr=subprocess.DEVNULL)
            ciphertext = encrypt(b"dummy payload", public)
            with self.assertRaises(ValueError):
                reader.decrypt(ciphertext, private)
            with patch.object(reader, "KEY_FINGERPRINT", hashlib.sha256(public.read_bytes()).hexdigest()):
                self.assertEqual(reader.decrypt(ciphertext, private), b"dummy payload")
                with self.assertRaises(subprocess.CalledProcessError):
                    reader.decrypt(bytes([ciphertext[0] ^ 1]) + ciphertext[1:], private)
                private.chmod(0o640)
                with self.assertRaises(ValueError):
                    reader.decrypt(ciphertext, private)
                private.chmod(0o604)
                with self.assertRaises(ValueError):
                    reader.decrypt(ciphertext, private)
                private.chmod(0o600)
                with patch.object(reader.os, "geteuid", return_value=os.geteuid() + 1):
                    with self.assertRaises(ValueError):
                        reader.decrypt(ciphertext, private)
                link = Path(directory) / "link"
                link.symlink_to(private)
                with self.assertRaises(OSError):
                    reader.decrypt(ciphertext, link)


if __name__ == "__main__":
    unittest.main()

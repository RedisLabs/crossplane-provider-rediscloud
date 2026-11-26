import hashlib
import json
import unittest

from qa_origin_candidates import select


class CandidateTests(unittest.TestCase):
    def setUp(self):
        self.credentials = json.dumps({"url": "https://qa.example.test/v1", "api_key": "dummy"})
        self.one = {"token": "a" * 32, "sha256": hashlib.sha256(b"https://qa.example.test").hexdigest()}
        self.two = {"token": "b" * 32, "sha256": hashlib.sha256(b"https://other.example.test").hexdigest()}

    def test_unique_match(self):
        self.assertEqual(select(self.credentials, json.dumps([self.one, self.two])), "a" * 32)

    def test_no_match_and_duplicate_rejected(self):
        for items in ([self.two], [self.one, self.one], [self.one, {**self.two, "token": self.one["token"]}], []):
            with self.subTest(items=items), self.assertRaises(ValueError):
                select(self.credentials, json.dumps(items))

    def test_invalid_shapes_and_credential_rejected(self):
        for raw in ("{}", "[]", '[{"token":"a","token":"b"}]', json.dumps([{"token": "host-name", "sha256": self.one["sha256"]}]),
                    json.dumps([{"token": "a" * 32, "sha256": "invalid"}]), json.dumps([self.one] * 17)):
            with self.subTest(raw=raw), self.assertRaises(ValueError):
                select(self.credentials, raw)
        with self.assertRaises(ValueError):
            select('{"url":"https://qa.example.test/v1?x=1"}', json.dumps([self.one]))


if __name__ == "__main__":
    unittest.main()

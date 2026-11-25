import hashlib
import json
import unittest

from qa_origin_binding import matches


class OriginBindingTests(unittest.TestCase):
    def setUp(self):
        self.digest = hashlib.sha256(b"https://qa.example.test").hexdigest()

    def check(self, url):
        return matches(json.dumps({"url": url, "api_key": "dummy", "secret_key": "dummy"}), self.digest)

    def test_exact_origin_and_v1_path(self):
        self.assertTrue(self.check("https://qa.example.test/v1"))
        self.assertTrue(self.check("https://qa.example.test/v1/"))
        self.assertFalse(self.check("https://other.example.test/v1"))

    def test_malformed_credential_or_digest_fails(self):
        for raw, digest in (("not-json", self.digest), ("{}", self.digest),
                            ('{"url": 5}', self.digest), ('{"url": "https://qa.example.test/v1"}', "bad")):
            with self.subTest(raw=raw), self.assertRaises((ValueError, TypeError)):
                matches(raw, digest)

    def test_noncanonical_or_unsafe_url_fails(self):
        for url in ("http://qa.example.test/v1", "https://qa.example.test:443/v1",
                    "https://user@qa.example.test/v1", "https://qa.example.test/v2",
                    "https://qa.example.test/v1?x=1", "https://qa.example.test/v1#x",
                    "https://QA.example.test/v1", "https://qa.example.test/v1\n"):
            with self.subTest(url=url), self.assertRaises(ValueError):
                self.check(url)


if __name__ == "__main__":
    unittest.main()

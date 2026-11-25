"""Compare a protected exact QA origin with a credential URL; never call a network."""
import hashlib
import hmac
import json
import os
import re
import sys
from urllib.parse import urlsplit


def matches(raw_credentials, expected_digest):
    if not re.fullmatch(r"[0-9a-f]{64}", expected_digest or ""):
        raise ValueError("invalid expected digest")
    credentials = json.loads(raw_credentials)
    if not isinstance(credentials, dict) or not isinstance(credentials.get("url"), str):
        raise ValueError("invalid credentials")
    raw_url = credentials["url"]
    parsed = urlsplit(raw_url)
    if (parsed.scheme != "https" or not parsed.hostname or parsed.netloc != parsed.hostname
            or parsed.path not in ("/v1", "/v1/") or parsed.query or parsed.fragment
            or raw_url != "https://" + parsed.netloc + parsed.path):
        raise ValueError("invalid endpoint")
    digest = hashlib.sha256(("https://" + parsed.netloc).encode("ascii")).hexdigest()
    return hmac.compare_digest(digest, expected_digest)


def main():
    try:
        matched = matches(os.environ.get("UPTEST_CLOUD_CREDENTIALS", ""),
                          os.environ.get("QA_TARGET_ORIGIN_SHA256", ""))
    except Exception:  # Do not print input or an exception containing it.
        print("BLOCKED: origin comparison input invalid", file=sys.stderr)
        return 1
    if not matched:
        print("BLOCKED: credential origin does not match target", file=sys.stderr)
        return 1
    print("PASS: credential origin matches independently protected target")
    return 0


if __name__ == "__main__":
    sys.exit(main())

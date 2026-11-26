"""No-network comparison against a bounded protected candidate set."""
import json
import os
import re
import sys

from qa_origin_binding import matches


def unique_object(pairs):
    obj = {}
    for key, value in pairs:
        if key in obj:
            raise ValueError("duplicate field")
        obj[key] = value
    return obj


def select(raw_credentials, raw_candidates):
    candidates = json.loads(raw_candidates, object_pairs_hook=unique_object)
    if not isinstance(candidates, list) or not 1 <= len(candidates) <= 16:
        raise ValueError("invalid candidate set")
    labels, digests, selected = set(), set(), []
    for item in candidates:
        if not isinstance(item, dict) or set(item) != {"token", "sha256"}:
            raise ValueError("invalid candidate")
        token, digest = item["token"], item["sha256"]
        if not isinstance(token, str) or not re.fullmatch(r"[a-f0-9]{32}", token):
            raise ValueError("invalid opaque token")
        if not isinstance(digest, str) or not re.fullmatch(r"[a-f0-9]{64}", digest):
            raise ValueError("invalid digest")
        if token in labels or digest in digests:
            raise ValueError("duplicate candidate")
        labels.add(token)
        digests.add(digest)
        if matches(raw_credentials, digest):
            selected.append(token)
    if len(selected) != 1:
        raise ValueError("not exactly one candidate")
    return selected[0]


def main():
    try:
        token = select(os.environ.get("UPTEST_CLOUD_CREDENTIALS", ""),
                       os.environ.get("QA_ORIGIN_CANDIDATES", ""))
    except Exception:  # Never emit raw inputs or error details.
        print("BLOCKED: invalid or nonunique candidate comparison", file=sys.stderr)
        return 1
    print("MATCH token=" + token)
    return 0


if __name__ == "__main__":
    sys.exit(main())

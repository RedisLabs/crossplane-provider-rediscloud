#!/usr/bin/env python3
"""Experimental one-shot DELETE fence; not connected to Cloud mutation.

The store's create operation must be atomic and existing markers must not be
modifiable or removable. This module cannot establish those repository rules.
"""
import hashlib
import hmac
import json
import re
import subprocess


class FenceUncertain(RuntimeError):
    """No permission to mutate: marker state or write outcome is uncertain."""


class GitHubRefStore:
    """Create and read refs without logging tokens, API errors, or identities.

    Separate repository rules must deny ref updates and deletes, including by
    the creating credential. Do not use this class to enable Cloud deletion
    until those rules and token permissions are independently verified.
    """
    def __init__(self, repository, run=subprocess.run):
        if not re.fullmatch(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+", repository):
            raise FenceUncertain("invalid repository")
        self.repository = repository
        self.run = run

    def create(self, ref, sha):
        payload = json.dumps({"ref": ref, "sha": sha})
        result = self.run(["gh", "api", "--method", "POST",
                           f"repos/{self.repository}/git/refs", "--input", "-"],
                          input=payload, text=True, capture_output=True, timeout=30)
        if result.returncode != 0:
            raise FenceUncertain("fence creation rejected or uncertain")
        body = json.loads(result.stdout)
        return body.get("ref") == ref and body.get("object", {}).get("sha") == sha

    def read(self, ref):
        # Inputs reach here only after reserve_once validates the full ref.
        result = self.run(["gh", "api", "--method", "GET",
                           f"repos/{self.repository}/git/ref/{ref[5:]}"],
                          text=True, capture_output=True, timeout=30)
        if result.returncode != 0:
            raise FenceUncertain("fence read unavailable")
        body = json.loads(result.stdout)
        if body.get("ref") != ref or body.get("object", {}).get("type") != "commit":
            raise FenceUncertain("fence read mismatch")
        return body["object"].get("sha")


def fence_ref(key, origin_hash, account_binding, subscription_id, database_id, operation):
    """Stable opaque key across source runs and recovery attempts."""
    if (not isinstance(key, bytes) or len(key) != 32 or
            not all(isinstance(s, str) and re.fullmatch(r"[0-9a-f]{64}", s)
                    for s in (origin_hash, account_binding)) or
            not re.fullmatch(r"[1-9][0-9]*", str(subscription_id)) or
            operation not in ("database", "subscription")):
        raise FenceUncertain("invalid fence identity")
    if operation == "database" and not re.fullmatch(r"[1-9][0-9]*", str(database_id)):
        raise FenceUncertain("invalid child identity")
    # The parent fence is shared across all children of the same subscription.
    child = str(database_id) if operation == "database" else ""
    payload = json.dumps(["redis-cloud-qa-delete-fence-v1", origin_hash,
                          account_binding, str(subscription_id), child, operation],
                         separators=(",", ":"), ensure_ascii=True).encode()
    return "refs/tags/qa-delete-fence/" + hmac.new(key, payload, hashlib.sha256).hexdigest()


def reserve_once(store, ref, commit_sha):
    """Atomically create then verify a marker; any ambiguity forbids DELETE.

    `store.create` must return exactly True only after a confirmed atomic create;
    an existing marker, timeout, retry, or malformed response fails closed. The
    caller must never delete a fence. This is deliberately not a lease.
    """
    if not re.fullmatch(r"refs/tags/qa-delete-fence/[0-9a-f]{64}", ref):
        raise FenceUncertain("invalid fence ref")
    if not re.fullmatch(r"[0-9a-f]{40}", commit_sha):
        raise FenceUncertain("invalid fence target")
    try:
        created = store.create(ref, commit_sha)
        if created is not True:
            raise FenceUncertain("fence not newly created")
        if store.read(ref) != commit_sha:
            raise FenceUncertain("fence readback differs")
    except FenceUncertain:
        raise
    except Exception as exc:
        raise FenceUncertain("fence persistence is uncertain") from exc


def inspect_fence(store, ref, expected_sha):
    """Read-only guidance; neither an absent nor present marker authorizes DELETE."""
    if (not re.fullmatch(r"refs/tags/qa-delete-fence/[0-9a-f]{64}", ref) or
            not re.fullmatch(r"[0-9a-f]{40}", expected_sha)):
        return "unknown"
    try:
        found = store.read(ref)
    except Exception:
        return "unknown"
    if found is None:
        return "absent-unverified"
    if found == expected_sha:
        return "reserved-manual-review"
    return "unknown"

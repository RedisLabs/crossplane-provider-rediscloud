"""Authenticated GitHub ciphertext reader. Never print a protected origin/digest."""
import hashlib
import io
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import zipfile

REPO = "RedisLabs/crossplane-provider-rediscloud"
BRANCH = "codex/qa-origin-ciphertext"
KEY_FINGERPRINT = "b344926eb2e9e4310638451d0a08998931812cc0d23fe93f7ab037c2aaf61b38"


def verify_metadata(run, jobs, artifacts, run_id, attempt, sha, artifact_id):
    if (not re.fullmatch(r"[1-9][0-9]*", run_id) or not re.fullmatch(r"[1-9][0-9]*", attempt)
            or not re.fullmatch(r"[1-9][0-9]*", artifact_id) or not re.fullmatch(r"[0-9a-f]{40}", sha)):
        raise ValueError("invalid expected metadata")
    if (str(run.get("id")) != run_id or str(run.get("run_attempt")) != attempt
            or run.get("head_sha") != sha or run.get("path") != ".github/workflows/ci.yml"
            or run.get("head_branch") != BRANCH or run.get("event") != "workflow_dispatch"
            or (run.get("actor") or {}).get("login") != "shay-berman"
            or (run.get("triggering_actor") or {}).get("login") != "shay-berman"
            or (run.get("repository") or {}).get("full_name") != REPO
            or (run.get("head_repository") or {}).get("full_name") != REPO
            or run.get("status") != "completed" or run.get("conclusion") != "success"):
        raise ValueError("run metadata mismatch")
    if not isinstance(jobs.get("jobs"), list) or jobs.get("total_count") != len(jobs["jobs"]):
        raise ValueError("incomplete jobs")
    job = [x for x in jobs["jobs"] if x.get("name") == "qa-origin-ciphertext"]
    if (len(job) != 1 or job[0].get("status") != "completed" or job[0].get("conclusion") != "success"
            or str(job[0].get("run_id")) != run_id or str(job[0].get("run_attempt")) != attempt
            or job[0].get("head_sha") != sha):
        raise ValueError("job not verified")
    if not isinstance(artifacts.get("artifacts"), list) or artifacts.get("total_count") != len(artifacts["artifacts"]):
        raise ValueError("incomplete artifacts")
    name = f"qa-origin-ciphertext-{run_id}-{attempt}"
    found = [x for x in artifacts["artifacts"] if x.get("name") == name]
    if (len(found) != 1 or str(found[0].get("id")) != artifact_id or found[0].get("expired") is not False
            or not isinstance(found[0].get("size_in_bytes"), int) or not 1 <= found[0]["size_in_bytes"] <= 1048576
            or not re.fullmatch(r"sha256:[0-9a-f]{64}", found[0].get("digest") or "")
            or str((found[0].get("workflow_run") or {}).get("id")) != run_id
            or (found[0].get("workflow_run") or {}).get("head_sha") != sha
            or (found[0].get("workflow_run") or {}).get("head_branch") != BRANCH):
        raise ValueError("artifact metadata mismatch")
    return found[0]["digest"]


def extract_ciphertext(archive, expected_digest):
    if len(archive) > 1048576 or "sha256:" + hashlib.sha256(archive).hexdigest() != expected_digest:
        raise ValueError("archive integrity mismatch")
    with zipfile.ZipFile(io.BytesIO(archive)) as bundle:
        entries = bundle.infolist()
        if len(entries) != 1 or entries[0].filename != "qa-origin-ciphertext.bin" or entries[0].file_size != 384:
            raise ValueError("unexpected archive")
        return bundle.read(entries[0])


def checked_payload(plaintext, run_id, attempt, sha):
    def unique_object(pairs):
        result = {}
        for key, value in pairs:
            if key in result:
                raise ValueError("duplicate field")
            result[key] = value
        return result
    payload = json.loads(plaintext, object_pairs_hook=unique_object)
    if (not isinstance(payload, dict) or set(payload) != {"purpose", "run", "attempt", "sha", "digest"}
            or payload["purpose"] != "qa-origin-candidate-v1" or payload["run"] != run_id
            or payload["attempt"] != attempt or payload["sha"] != sha
            or not isinstance(payload["digest"], str) or not re.fullmatch(r"[0-9a-f]{64}", payload["digest"])):
        raise ValueError("ciphertext context mismatch")
    return payload["digest"]


def decrypt(ciphertext, private_key):
    fd = os.open(private_key, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        info = os.fstat(fd)
        if not stat.S_ISREG(info.st_mode) or info.st_uid != os.geteuid() or info.st_mode & 0o077:
            raise ValueError("private key custody invalid")
        key_fd = f"/proc/self/fd/{fd}"
        public = subprocess.run(["openssl", "pkey", "-in", key_fd, "-pubout"], pass_fds=(fd,),
                                stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=True).stdout
        if hashlib.sha256(public).hexdigest() != KEY_FINGERPRINT:
            raise ValueError("private key mismatch")
        os.lseek(fd, 0, os.SEEK_SET)
        return subprocess.run(["openssl", "pkeyutl", "-decrypt", "-inkey", key_fd,
                               "-pkeyopt", "rsa_padding_mode:oaep", "-pkeyopt", "rsa_oaep_md:sha256",
                               "-pkeyopt", "rsa_mgf1_md:sha256"], input=ciphertext, pass_fds=(fd,),
                              stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=True).stdout
    finally:
        os.close(fd)


def gh_json(path):
    raw = subprocess.run(["gh", "api", path], stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=True).stdout
    return json.loads(raw)


def read_verified(run_id, attempt, sha, artifact_id, private_key):
    if (not re.fullmatch(r"[1-9][0-9]*", run_id) or not re.fullmatch(r"[1-9][0-9]*", attempt)
            or not re.fullmatch(r"[1-9][0-9]*", artifact_id) or not re.fullmatch(r"[0-9a-f]{40}", sha)):
        raise ValueError("invalid expected metadata")
    base = f"repos/{REPO}/actions"
    run = gh_json(f"{base}/runs/{run_id}")
    jobs = gh_json(f"{base}/runs/{run_id}/attempts/{attempt}/jobs?per_page=100")
    artifacts = gh_json(f"{base}/runs/{run_id}/artifacts?per_page=100")
    expected_digest = verify_metadata(run, jobs, artifacts, run_id, attempt, sha, artifact_id)
    archive = subprocess.run(["gh", "api", f"{base}/artifacts/{artifact_id}/zip"], stdout=subprocess.PIPE,
                             stderr=subprocess.DEVNULL, check=True).stdout
    ciphertext = extract_ciphertext(archive, expected_digest)
    return checked_payload(decrypt(ciphertext, private_key), run_id, attempt, sha)


def main():
    # This reader deliberately does not print the protected digest. A separate,
    # reviewed local mapping step is required before any candidate conclusion.
    if len(sys.argv) != 6:
        print("BLOCKED: reader requires run, attempt, reviewed SHA, artifact ID and private key", file=sys.stderr)
        return 1
    try:
        run_id, attempt, sha, artifact_id, private_key = sys.argv[1:]
        read_verified(run_id, attempt, sha, artifact_id, Path(private_key))
    except Exception:
        print("BLOCKED: origin evidence verification failed", file=sys.stderr)
        return 1
    print("PASS: authenticated ciphertext context verified; origin not disclosed")
    return 0


if __name__ == "__main__":
    sys.exit(main())

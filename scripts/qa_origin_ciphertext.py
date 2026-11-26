"""No-network protected origin extraction encrypted to a reviewed public key."""
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

from qa_origin_binding import matches


def selected_digest(raw_credentials, raw_allowlist):
    candidates = raw_allowlist.split(",")
    if not 1 <= len(candidates) <= 16 or any(not re.fullmatch(r"[0-9a-f]{64}", x) for x in candidates):
        raise ValueError("invalid allowlist")
    if len(set(candidates)) != len(candidates):
        raise ValueError("duplicate allowlist")
    selected = [digest for digest in candidates if matches(raw_credentials, digest)]
    if len(selected) != 1:
        raise ValueError("not exactly one allowed origin")
    return selected[0]


def build_payload(digest, env):
    if not re.fullmatch(r"[0-9a-f]{64}", digest):
        raise ValueError("invalid digest")
    run_id, attempt, sha = env.get("GITHUB_RUN_ID", ""), env.get("GITHUB_RUN_ATTEMPT", ""), env.get("GITHUB_SHA", "")
    if (not re.fullmatch(r"[1-9][0-9]*", run_id) or not re.fullmatch(r"[1-9][0-9]*", attempt)
            or not re.fullmatch(r"[0-9a-f]{40}", sha)
            or env.get("GITHUB_EVENT_NAME") != "workflow_dispatch"
            or env.get("GITHUB_REPOSITORY") != "RedisLabs/crossplane-provider-rediscloud"
            or env.get("GITHUB_ACTOR") != "shay-berman"
            or env.get("GITHUB_REF") != "refs/heads/codex/qa-origin-ciphertext"):
        raise ValueError("invalid run context")
    return json.dumps({"purpose": "qa-origin-candidate-v1", "run": run_id, "attempt": attempt,
                       "sha": sha, "digest": digest}, sort_keys=True, separators=(",", ":")).encode("ascii")


def encrypt(payload, public_key):
    if not 1 <= len(payload) <= 250:
        raise ValueError("invalid payload size")
    result = subprocess.run(
        ["openssl", "pkeyutl", "-encrypt", "-pubin", "-inkey", str(public_key),
         "-pkeyopt", "rsa_padding_mode:oaep", "-pkeyopt", "rsa_oaep_md:sha256",
         "-pkeyopt", "rsa_mgf1_md:sha256"], input=payload,
        stdout=subprocess.PIPE, stderr=subprocess.DEVNULL, check=True,
    )
    if len(result.stdout) != 384:
        raise ValueError("invalid ciphertext size")
    return result.stdout


def main():
    try:
        digest = selected_digest(os.environ.get("UPTEST_CLOUD_CREDENTIALS", ""),
                                 os.environ.get("QA_VERIFIED_ORIGIN_HASHES", ""))
        public_key = Path(__file__).with_name("qa_origin_public.pem")
        if hashlib.sha256(public_key.read_bytes()).hexdigest() != "b344926eb2e9e4310638451d0a08998931812cc0d23fe93f7ab037c2aaf61b38":
            raise ValueError("public key differs")
        ciphertext = encrypt(build_payload(digest, os.environ), public_key)
        output = Path(os.environ["RUNNER_TEMP"]) / "qa-origin-ciphertext.bin"
        fd = os.open(output, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, "wb") as stream:
            stream.write(ciphertext)
    except Exception:  # Never print protected values or error details.
        print("BLOCKED: protected origin encryption unavailable", file=sys.stderr)
        return 1
    print("PASS: encrypted origin evidence prepared; no network request")
    return 0


if __name__ == "__main__":
    sys.exit(main())

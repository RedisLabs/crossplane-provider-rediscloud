#!/usr/bin/env python3
"""Verify an encrypted checkpoint belongs to a completed QA workflow attempt."""
import json
import re
import subprocess
import sys
from pathlib import Path


def verify(intent, run):
    if (intent.get("kind") != "strict" or intent.get("repo") != "RedisLabs/crossplane-provider-rediscloud" or
            str(run.get("id")) != intent.get("runId") or
            str(run.get("run_attempt")) != intent.get("attempt") or
            run.get("head_sha") != intent.get("commit") or
            run.get("path") != ".github/workflows/qa-live.yaml" or
            run.get("event") != "workflow_dispatch" or run.get("status") != "completed"):
        raise ValueError("source run not verified")


def main():
    try:
        path, run_id, attempt = sys.argv[1:]
        if not re.fullmatch(r"[1-9][0-9]*", run_id) or not re.fullmatch(r"[1-9][0-9]*", attempt):
            raise ValueError("invalid run identity")
        intent = json.loads(Path(path).read_text())
        if intent.get("runId") != run_id or intent.get("attempt") != attempt:
            raise ValueError("checkpoint run mismatch")
        endpoint = f"repos/RedisLabs/crossplane-provider-rediscloud/actions/runs/{run_id}/attempts/{attempt}"
        result = subprocess.run(["gh", "api", endpoint], check=True, capture_output=True, text=True)
        verify(intent, json.loads(result.stdout))
        print("source QA workflow attempt authenticated and completed")
        return 0
    except (ValueError, KeyError, OSError, subprocess.CalledProcessError):
        print("source workflow could not be authenticated; no recovery mutation allowed", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())

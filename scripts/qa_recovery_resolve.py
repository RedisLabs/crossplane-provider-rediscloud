#!/usr/bin/env python3
"""Read-only recovery discovery from an authenticated ownership intent.

Absence is never treated as proof after runner loss: an asynchronous Cloud
creation may still complete after the originating worker stopped.
"""
import argparse
import base64
import hashlib
import hmac
import json
import os
import re
import sys
import urllib.parse
from pathlib import Path

import qa_live


def resolve(cloud, intent, log):
    name = intent["runName"]
    matches = [row for row in cloud.list_fixed_subscriptions_checked() if row.get("name") == name]
    if not matches:
        log("ownership resolution: no subscription visible; asynchronous creation remains possible")
        return "uncertain"
    if len(matches) != 1:
        raise qa_live.SuiteFailure("ownership resolution ambiguous: duplicate subscription names")
    subscription = matches[0]
    sub_id = str(subscription.get("id") or "")
    if not re.fullmatch(r"[0-9]+", sub_id):
        raise qa_live.SuiteFailure("ownership resolution ambiguous: subscription identity invalid")
    code, body = cloud.get_fixed_subscription(sub_id)
    if code != 200 or not isinstance(body, dict) or str(body.get("id") or "") != sub_id or body.get("name") != name:
        raise qa_live.SuiteFailure("ownership resolution ambiguous: subscription read does not match")
    code, rows = cloud.list_fixed_databases_complete(sub_id)
    if code != 200:
        raise qa_live.SuiteFailure("ownership resolution ambiguous: incomplete database inventory")
    owned = [row for row in rows if row.get("name") == f"{name}-db"]
    if len(owned) > 1:
        raise qa_live.SuiteFailure("ownership resolution ambiguous: duplicate database names")
    foreign = len(rows) - len(owned)
    if not owned:
        log(f"ownership resolution: subscription present; run database not visible; foreign databases={foreign}; asynchronous creation remains possible")
        return "uncertain"
    db_id = qa_live.observed_database_id(owned[0], "")
    code, body = cloud.get_fixed_database(sub_id, db_id)
    if (code != 200 or not isinstance(body, dict) or
            str(body.get("databaseId") or "") != db_id or body.get("name") != f"{name}-db"):
        raise qa_live.SuiteFailure("ownership resolution ambiguous: exact database read does not match")
    log(f"ownership resolution: subscription and database present; foreign databases={foreign}; no mutation performed")
    return "present"


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("intent")
    args = parser.parse_args()
    try:
        intent = json.loads(Path(args.intent).read_text())
        credentials = qa_live.load_credentials(None, os.environ.get("UPTEST_CLOUD_CREDENTIALS"))
        url = credentials.get("url") or ""
        if not qa_live.matches_expected_qa_origin(url):
            raise qa_live.SuiteFailure("recovery requires verified QA endpoint")
        parts = urllib.parse.urlsplit(url)
        origin = f"{parts.scheme}://{parts.netloc}"
        if hashlib.sha256(origin.encode()).hexdigest() != intent.get("originHash"):
            raise qa_live.SuiteFailure("checkpoint and credential QA endpoints differ")
        key = base64.b64decode(os.environ.get("QA_CHECKPOINT_KEY", ""), validate=True)
        if len(key) != 32:
            raise qa_live.SuiteFailure("checkpoint key invalid")
        binding = hmac.new(key, credentials["api_key"].encode(), hashlib.sha256).hexdigest()
        if not hmac.compare_digest(binding, str(intent.get("credentialBinding") or "")):
            raise qa_live.SuiteFailure("checkpoint credential binding differs")
        cloud = qa_live.RedisCloud(url, credentials["api_key"], credentials["secret_key"])
        result = resolve(cloud, intent, print)
        # A successful discovery is not cleanup; a missing resource after
        # runner loss remains unresolved while creation could still finish.
        return 0 if result == "present" else 2
    except (qa_live.SuiteFailure, OSError, ValueError, KeyError):
        print("recovery discovery failed or is ambiguous; no mutation performed", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())

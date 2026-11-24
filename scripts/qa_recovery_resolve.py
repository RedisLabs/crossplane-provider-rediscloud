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
import subprocess
import sys
import tempfile
import time
import urllib.parse
from pathlib import Path
from datetime import datetime, timedelta, timezone

import qa_live
import qa_capture_task_baseline
import qa_verify_source


def resolve(cloud, intent, log, details=None):
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
    subscription_status = body.get("status")
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
    if details is not None:
        details.update(subscriptionId=sub_id, databaseId=db_id,
                       subscriptionStatus=subscription_status, databaseStatus=body.get("status"), foreign=foreign)
    log(f"ownership resolution: subscription and database present; foreign databases={foreign}; no mutation performed")
    return "present"


def task_delta(cloud, intent, log):
    captured = datetime.strptime(intent["taskBaseline"]["capturedAt"], "%Y-%m-%dT%H:%M:%SZ").replace(tzinfo=timezone.utc)
    age = datetime.now(timezone.utc) - captured
    if age < timedelta(0) or age >= timedelta(days=10):
        raise qa_live.SuiteFailure("task baseline time falls outside documented task retention")
    current_tasks = qa_capture_task_baseline.task_rows(cloud)
    prior = set(intent["taskBaseline"]["taskIds"])
    current = set(current_tasks)
    log(f"account task delta: new={len(current - prior)} missing_baseline={len(prior - current)}; ownership and provisioning remain unverified")
    return {task_id: current_tasks[task_id] for task_id in current - prior}, len(prior - current)


def task_evidence(cloud, new_tasks, missing_baseline, details, log):
    """Correlate candidates only; this does not authorize Cloud mutation."""
    if missing_baseline or not details or details.get("foreign"):
        log("create-task correlation uncertain: baseline or ownership is incomplete")
        return False
    candidates = {kind: [(task_id, row) for task_id, row in new_tasks.items() if row.get("commandType") == kind]
                  for kind in ("fixedSubscriptionCreateRequest", "fixedDatabaseCreateRequest")}
    if len(new_tasks) != 2 or any(len(rows) != 1 for rows in candidates.values()):
        log("create-task correlation uncertain: expected exactly one task of each kind")
        return False
    expected = (("fixedSubscriptionCreateRequest", details["subscriptionId"], None),
                ("fixedDatabaseCreateRequest", details["databaseId"], details["subscriptionId"]))
    for kind, resource_id, parent_id in expected:
        task_id, row = candidates[kind][0]
        code, current = cloud.request("GET", f"/tasks/{task_id}")
        if code != 200 or not isinstance(current, dict) or str(current.get("taskId") or "").lower() != task_id:
            log("create-task correlation uncertain: exact task read unavailable")
            return False
        response = current.get("response") or {}
        if (current.get("commandType") != kind or current.get("status") != "processing-completed" or
                not isinstance(response, dict) or str(response.get("resourceId") or "") != resource_id or
                (parent_id is not None and str(response.get("additionalResourceId") or "") != parent_id)):
            log("create-task correlation uncertain: task status or identity does not match")
            return False
    if details.get("subscriptionStatus") != "active" or details.get("databaseStatus") != "active":
        log("create-task correlation uncertain: resource provisioning is not active")
        return False
    log("create-task candidates match exact active resources; manual recovery remains required")
    return True


def discover(cloud, intent, log):
    task_check_ok = True
    try:
        new_tasks, missing = task_delta(cloud, intent, log)
    except (qa_live.SuiteFailure, KeyError, TypeError, ValueError):
        task_check_ok = False
        log("account task delta unavailable; ownership and provisioning remain unverified")
    details = {}
    result = resolve(cloud, intent, log, details)
    evidence_ok = False
    if task_check_ok and result == "present":
        evidence_ok = task_evidence(cloud, new_tasks, missing, details, log)
    return 0 if evidence_ok else 2


def recover_exact(cloud, intent, log, timeout=900, poll=15, now=time.monotonic, sleep=time.sleep):
    """Recover one proven pair; never infer ownership from a shared prefix.

    The workflow authenticates the source; the coordinator serializes writers.
    """
    new_tasks, missing = task_delta(cloud, intent, log)
    details = {}
    if resolve(cloud, intent, log, details) != "present":
        raise qa_live.SuiteFailure("recovery uncertain: both original resources must be present")
    if not task_evidence(cloud, new_tasks, missing, details, log):
        raise qa_live.SuiteFailure("recovery uncertain: exact create-task evidence is incomplete")
    sub_id, db_id = details["subscriptionId"], details["databaseId"]
    name = intent["runName"]
    log("RECOVERY ONLY: original qualification remains failed; deleting exact owned database")
    cloud.delete_fixed_database(sub_id, db_id)
    deadline = now() + timeout
    while True:
        parent_code, parent = cloud.get_fixed_subscription(sub_id)
        db_code, _ = cloud.get_fixed_database(sub_id, db_id)
        if (parent_code != 200 or not isinstance(parent, dict) or str(parent.get("id") or "") != sub_id
                or parent.get("name") != name):
            raise qa_live.SuiteFailure("recovery stopped: parent identity or read changed")
        if db_code == 404:
            break
        if db_code != 200 or now() >= deadline:
            raise qa_live.SuiteFailure("recovery stopped: database absence unverified")
        sleep(poll)
    code, rows = cloud.list_fixed_databases_complete(sub_id)
    if code != 200 or rows:
        raise qa_live.SuiteFailure("recovery stopped: child inventory is not completely empty")
    # Verify the exact parent again after child deletion and before mutation.
    code, parent = cloud.get_fixed_subscription(sub_id)
    if (code != 200 or not isinstance(parent, dict) or str(parent.get("id") or "") != sub_id
            or parent.get("name") != name or parent.get("status") != "active"):
        raise qa_live.SuiteFailure("recovery stopped: exact parent is not active")
    log("RECOVERY ONLY: database absent while parent exists; deleting exact owned subscription")
    cloud.delete_fixed_subscription(sub_id)
    deadline = now() + timeout
    while True:
        code, _ = cloud.get_fixed_subscription(sub_id)
        if code == 404:
            break
        if code != 200 or now() >= deadline:
            raise qa_live.SuiteFailure("recovery stopped: subscription absence unverified")
        sleep(poll)
    if any(row.get("name") == name for row in cloud.list_fixed_subscriptions_checked()):
        raise qa_live.SuiteFailure("recovery stopped: subscription remains in inventory")
    log("RECOVERY ONLY: exact pair absent; original qualification remains failed")


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("intent")
    parser.add_argument("--recover-exact", action="store_true")
    parser.add_argument("--source-run-id", default="")
    parser.add_argument("--source-attempt", default="")
    args = parser.parse_args()
    try:
        if args.recover_exact:
            if not args.source_run_id or not args.source_attempt:
                raise qa_live.SuiteFailure("recovery requires the source run and attempt")
            # Re-authenticate the encrypted artifact inside the mutating path;
            # never trust a caller-supplied plaintext ownership file.
            with tempfile.TemporaryDirectory(prefix="qa-recovery-") as tmp:
                encrypted = Path(tmp) / "ownership.enc"
                encrypted.write_bytes(Path(args.intent).read_bytes())
                script = Path(__file__).with_name("qa_checkpoint.mjs")
                subprocess.run(["node", str(script), "resolve", str(encrypted), args.source_run_id,
                                args.source_attempt], check=True, capture_output=True, text=True)
                intent = json.loads(Path(str(encrypted) + ".resolved").read_text())
            qa_verify_source.verify_remote(intent, args.source_run_id, args.source_attempt)
        else:
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
        # A successful discovery is not cleanup; a missing resource after
        # runner loss remains unresolved while creation could still finish.
        if args.recover_exact:
            recover_exact(cloud, intent, print)
            return 0
        return discover(cloud, intent, print)
    except (qa_live.SuiteFailure, OSError, ValueError, KeyError, subprocess.CalledProcessError):
        print("recovery incomplete or ambiguous; consult the operation log and verify inventory", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())

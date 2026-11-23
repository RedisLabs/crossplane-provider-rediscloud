#!/usr/bin/env python3
"""Capture account task identities for encryption before any Cloud write."""
import json
import os
import sys
import time
import uuid

import qa_live


def task_rows(cloud):
    code, body = cloud.request("GET", "/tasks")
    if code != 200 or not isinstance(body, dict) or not isinstance(body.get("tasks"), list):
        raise qa_live.SuiteFailure("task baseline is unavailable")
    for key in ("next", "nextPage", "nextPageToken"):
        if body.get(key):
            raise qa_live.SuiteFailure("task baseline is paginated")
    if body.get("hasMore") or (isinstance(body.get("_links"), dict) and body["_links"].get("next")):
        raise qa_live.SuiteFailure("task baseline is paginated")
    rows = body["tasks"]
    for key in ("total", "totalCount"):
        if key in body and (not isinstance(body[key], int) or body[key] != len(rows)):
            raise qa_live.SuiteFailure("task baseline is incomplete")
    if body.get("offset", 0) != 0 or (isinstance(body.get("limit"), int) and len(rows) >= body["limit"]
                                      and "total" not in body and "totalCount" not in body):
        raise qa_live.SuiteFailure("task baseline pagination coverage is unverified")
    ids = {}
    for row in rows:
        task_id = row.get("taskId") if isinstance(row, dict) else None
        if not isinstance(task_id, str):
            raise qa_live.SuiteFailure("task baseline contains an invalid identity")
        try:
            normalized = str(uuid.UUID(task_id))
        except ValueError:
            raise qa_live.SuiteFailure("task baseline contains an invalid identity") from None
        canonical = json.dumps(row, sort_keys=True, separators=(",", ":"))
        if normalized in ids and ids[normalized] != canonical:
            raise qa_live.SuiteFailure("task baseline contains conflicting duplicate identities")
        ids[normalized] = canonical
        if len(ids) > 20000:
            raise qa_live.SuiteFailure("task baseline exceeds the identity limit")
    return {task_id: json.loads(row) for task_id, row in ids.items()}


def baseline(cloud):
    ids = task_rows(cloud)
    result = {"capturedAt": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()), "taskIds": sorted(ids)}
    if len(json.dumps(result).encode()) > 1024 * 1024:
        raise qa_live.SuiteFailure("task baseline exceeds the encrypted payload limit")
    return result


def main():
    try:
        credentials = qa_live.load_credentials(None, os.environ.get("UPTEST_CLOUD_CREDENTIALS"))
        url = credentials.get("url") or ""
        if not qa_live.matches_expected_qa_origin(url):
            raise qa_live.SuiteFailure("task baseline requires verified QA endpoint")
        cloud = qa_live.RedisCloud(url, credentials["api_key"], credentials["secret_key"])
        print(json.dumps(baseline(cloud), separators=(",", ":")))
        return 0
    except (qa_live.SuiteFailure, OSError, ValueError):
        print("task baseline unavailable; no Cloud write allowed", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())

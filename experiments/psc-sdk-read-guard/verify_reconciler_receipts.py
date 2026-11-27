"""Require actual fake-API reconciler failures and recoveries to execute."""
import json
from pathlib import Path
import sys

root = "TestPSCServiceMalformedTaskRecovery"
cases = ("initial", "missing_task_id", "recovery_one", "missing_resource", "recovery_two")
expected = {f"{root}/{case}" for case in cases}
endpoint_root = "TestPSCEndpointsMalformedTaskRecovery"
endpoint_cases = ("initial", "list_missing_task", "list_recovery_one", "list_missing_resource", "list_recovery_two", "script_missing_task", "script_recovery_one", "script_missing_resource", "script_recovery_two", "list_http_error", "http_recovery")
expected |= {f"{endpoint_root}/{scope}/{case}" for scope in ("pro", "active_active") for case in endpoint_cases}
events = [json.loads(line) for line in Path(sys.argv[1]).read_text().splitlines()]
passed = {e.get("Test") for e in events if e.get("Action") == "pass"}
failed = {e.get("Test") for e in events if e.get("Action") in {"fail", "skip"}}
if not expected <= passed or expected & failed or root not in passed or endpoint_root not in passed:
    raise SystemExit("missing, skipped or failed reconciler receipts")
print(f"Verified {len(expected)} named fake reconciler receipts without containment")

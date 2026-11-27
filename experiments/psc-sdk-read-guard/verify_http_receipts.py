"""Require all expected simulated HTTP cases to run and pass."""
import json
from pathlib import Path
import sys

root = "TestPSCReadTaskValidationHTTP"
cases = ("missing", "null", "empty", "empty_body", "invalid_json", "initial_forbidden", "initial_not_found", "task_forbidden", "missing_resource", "valid")
operations = ("service", "service_active_active", "endpoints", "endpoints_active_active", "creation_script", "creation_script_active_active", "deletion_script", "deletion_script_active_active")
expected = {f"{root}/{operation}/{case}" for operation in operations for case in cases}
events = [json.loads(line) for line in Path(sys.argv[1]).read_text().splitlines()]
passed = {e.get("Test") for e in events if e.get("Action") == "pass"}
failed = {e.get("Test") for e in events if e.get("Action") in {"fail", "skip"}}
if not expected <= passed or expected & failed or root not in passed:
    raise SystemExit("missing, skipped or failed HTTP receipts")
print(f"Verified {len(expected)} named fake HTTP receipts")

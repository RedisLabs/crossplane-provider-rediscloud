"""Require every named SDK read case to execute successfully on the runner."""
import json
from pathlib import Path
import sys

operations = (
    "service", "service_active_active", "endpoints", "endpoints_active_active",
    "creation_script", "creation_script_active_active", "deletion_script",
    "deletion_script_active_active",
)
cases = ("missing", "empty", "valid", "transport_error")
root = "TestPSCReadRejectsMissingTaskID"
expected = {f"{root}/{operation}/{case}" for operation in operations for case in cases}
events = [json.loads(line) for line in Path(sys.argv[1]).read_text().splitlines()]
passed = {event.get("Test") for event in events if event.get("Action") == "pass"}
failed = {event.get("Test") for event in events if event.get("Action") in {"fail", "skip"}}
if not expected <= passed or expected & failed or root not in passed:
    raise SystemExit("missing, skipped or failed SDK read receipts")
print(f"Verified {len(expected)} named fake SDK read receipts")

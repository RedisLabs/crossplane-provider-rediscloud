"""Require the exact fake managed-drift case and the expected outcome."""
import json
import os
import subprocess
import sys

PACKAGE = "github.com/RedisLabs/provider-rediscloud/scripts/qa_tfplan"
ROOT = "TestProDatabaseManagedLifecycleLoopback"
CASE = ROOT + "/source_cidr_drift"
MARKER = "observed CIDR drift produced an up-to-date plan"


def main() -> None:
    if os.environ.get("GITHUB_ACTIONS") != "true" or not all(
        os.environ.get(key) for key in ("QA_TEST_TERRAFORM", "QA_TEST_PROVIDER_DIR")
    ):
        raise ValueError("hosted pinned fixture required")
    if len(sys.argv) != 2 or sys.argv[1] not in ("candidate", "baseline"):
        raise ValueError("expected candidate or baseline")
    result = subprocess.run(
        ["go", "test", "-json", "./scripts/qa_tfplan", "-count=1", "-timeout=15m",
         "-run", "^" + ROOT + "$/^source_cidr_drift$"],
        capture_output=True, text=True, timeout=1200, check=False,
    )
    events = [json.loads(line) for line in result.stdout.splitlines()]
    if any(event.get("Package") != PACKAGE for event in events):
        raise ValueError("unexpected Go package")
    for event in events:
        if event.get("Output"):
            print(event["Output"], end="")
    if result.stderr:
        print(result.stderr, file=sys.stderr, end="")
    actions = [event.get("Action") for event in events if event.get("Test") == CASE
               and event.get("Action") in ("run", "pass", "fail", "skip")]
    if sys.argv[1] == "candidate":
        if result.returncode != 0 or actions != ["run", "pass"]:
            raise ValueError("candidate did not pass the exact drift case")
    else:
        output = "".join(event.get("Output", "") for event in events if event.get("Test") == CASE)
        if result.returncode == 0 or actions != ["run", "fail"] or MARKER not in output:
            raise ValueError("baseline did not fail at the expected drift detection assertion")


if __name__ == "__main__":
    try:
        main()
    except (OSError, ValueError, subprocess.TimeoutExpired) as exc:
        raise SystemExit(str(exc)) from exc

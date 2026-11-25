"""Require explicit Go test execution evidence before claiming fixture coverage."""
from __future__ import annotations

import json
import os
import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
MODULE = "github.com/RedisLabs/provider-rediscloud"


def validate_events(output: str, package: str, names: tuple[str, ...]) -> None:
    expected_package = MODULE + "/" + package.removeprefix("./")
    if not names or len(set(names)) != len(names):
        raise ValueError("test names must be nonempty and unique")
    events = {name: [] for name in names}
    package_pass = False
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError as exc:
            raise ValueError("invalid go test event stream") from exc
        if not isinstance(event, dict) or event.get("Package") != expected_package:
            raise ValueError("unexpected Go package event")
        action, name = event.get("Action"), event.get("Test", "")
        if not name and action == "pass":
            package_pass = True
        if name:
            for expected in names:
                if name == expected and action in ("run", "pass", "fail", "skip"):
                    events[expected].append(action)
                if name.startswith(expected + "/") and action in ("fail", "skip"):
                    raise ValueError("a selected subtest failed or skipped")
    if not package_pass or any(actions != ["run", "pass"] for actions in events.values()):
        raise ValueError("selected test did not explicitly run and pass")


def run_selected(package: str, names: tuple[str, ...], *, require_fixture: bool = False, offline: bool = False, timeout: int = 1200) -> None:
    if os.environ.get("GITHUB_ACTIONS") != "true":
        raise ValueError("integration fixture execution requires GitHub Actions")
    if require_fixture and not all(os.environ.get(key) for key in ("QA_TEST_TERRAFORM", "QA_TEST_PROVIDER_DIR")):
        raise ValueError("pinned fixture dependencies are required")
    if not names or any(not re.fullmatch(r"Test[A-Za-z0-9_]+(?:/[A-Za-z0-9_]+)*", name) for name in names):
        raise ValueError("invalid selected test names")
    roots = sorted({name.split("/", 1)[0] for name in names})
    pattern = "^(" + "|".join(re.escape(name) for name in roots) + ")$"
    env = os.environ.copy()
    if offline:
        env.update({"GOPROXY": "off", "GOSUMDB": "off"})
    run = subprocess.run(
        ["go", "test", "-json", package, "-run", pattern, "-count=1", "-timeout=15m"],
        cwd=ROOT, env=env, text=True, capture_output=True, timeout=timeout, check=False,
    )
    for line in run.stdout.splitlines():
        try:
            output = json.loads(line).get("Output", "")
        except (json.JSONDecodeError, AttributeError):
            output = ""
        if output:
            print(output, end="")
    if run.stderr:
        print(run.stderr, file=sys.stderr, end="")
    if run.returncode:
        raise ValueError("go test command failed")
    validate_events(run.stdout, package, names)


if __name__ == "__main__":
    try:
        run_selected(sys.argv[1], tuple(sys.argv[2:]), require_fixture=True)
    except (IndexError, OSError, subprocess.TimeoutExpired, ValueError) as exc:
        raise SystemExit(str(exc)) from exc

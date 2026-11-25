"""Provisional acceptance evidence contract; this module never runs a writer.

Evidence is a provider adapter's claim. Structural validity is not independent
authentication of a run, account, Cloud observation, artifact, or lock.
"""

from __future__ import annotations

import json
import re
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol
from urllib.parse import urlsplit


@dataclass(frozen=True)
class Scenario:
    id: str
    steps: tuple[str, ...]
    access: str = "write"


@dataclass(frozen=True)
class StepReceipt:
    step: str
    outcome: str  # passed, failed, blocked, unsupported
    evidence_url: str


@dataclass(frozen=True)
class Evidence:
    provider: str
    scenario_id: str
    mode: str  # live or simulated; never inferred from a test name
    source_revision: str
    artifact_digest: str
    run_url: str
    evidence_url: str
    steps: tuple[StepReceipt, ...]
    outcome: str  # pass, fail, blocked, unsupported
    cleanup: str  # clean, residual, unknown, not_started
    direct_cloud_recovery: bool


class Adapter(Protocol):
    """Future adapter boundary; no implementation in this proposal is live-enabled.

    The caller must arrange approved account-wide admission and an exact,
    durable recovery journal before invoking any implementation that writes.
    An adapter must return explicit unsupported/blocked results and never infer
    a clean inventory from a failed or partial read.
    """

    def execute(self, scenario: Scenario) -> Evidence: ...


def load_scenarios(path: Path) -> dict[str, Scenario]:
    data = json.loads(path.read_text(encoding="utf-8"))
    if not isinstance(data, dict) or set(data) != {"version", "scenarios"} or type(data["version"]) is not int or data["version"] not in (1, 2):
        raise ValueError("unsupported scenario document")
    if not isinstance(data["scenarios"], list) or not data["scenarios"]:
        raise ValueError("scenario list is empty or malformed")
    result = {}
    for entry in data["scenarios"]:
        keys = {"id", "steps"} if data["version"] == 1 else {"id", "steps", "access"}
        if not isinstance(entry, dict) or set(entry) != keys:
            raise ValueError("malformed scenario")
        name, steps = entry["id"], entry["steps"]
        access = entry.get("access", "write")
        if access not in ("write", "read_only"):
            raise ValueError("invalid scenario access")
        if not isinstance(name, str) or not re.fullmatch(r"[a-z0-9-]+", name):
            raise ValueError("invalid scenario id")
        if name in result or not isinstance(steps, list) or not steps:
            raise ValueError("duplicate scenario or malformed steps")
        if any(not isinstance(s, str) or not re.fullmatch(r"[a-z0-9_]+", s) for s in steps) or len(set(steps)) != len(steps):
            raise ValueError("invalid or duplicate step")
        if access == "write":
            if steps[0] != "admission_and_clean_baseline" or steps[-1] != "complete_clean_inventory":
                raise ValueError("scenario lacks safety boundaries")
        elif steps != ["read_only_preflight", "lookup_by_identity", "verify_expected_response", "verify_no_cloud_mutations"]:
            raise ValueError("read-only scenario has invalid steps")
        result[name] = Scenario(name, tuple(steps), access)
    return result


def classify_claim(scenario: Scenario, evidence: Evidence) -> str:
    """Validate an adapter claim without verifying its external evidence links.

    Returns a mode-qualified claim, an explicit non-pass, or invalid. Callers
    must authenticate the linked records separately before reporting a pass.
    """
    if evidence.provider not in {"terraform", "pulumi", "crossplane"} or evidence.scenario_id != scenario.id:
        return "invalid"
    if evidence.mode not in {"live", "simulated"} or evidence.outcome not in {"pass", "fail", "blocked", "unsupported"}:
        return "invalid"
    if evidence.cleanup not in {"clean", "residual", "unknown", "not_started"} or type(evidence.direct_cloud_recovery) is not bool:
        return "invalid"
    if evidence.outcome != "pass":
        return evidence.outcome
    if scenario.access not in {"write", "read_only"}:
        return "invalid"
    if scenario.access == "read_only" and scenario.steps != ("read_only_preflight", "lookup_by_identity", "verify_expected_response", "verify_no_cloud_mutations"):
        return "invalid"
    required_cleanup = "not_started" if scenario.access == "read_only" else "clean"
    if evidence.direct_cloud_recovery or evidence.cleanup != required_cleanup:
        return "invalid"
    if not isinstance(evidence.steps, tuple) or len(evidence.steps) != len(scenario.steps):
        return "invalid"
    for expected, receipt in zip(scenario.steps, evidence.steps):
        if not isinstance(receipt, StepReceipt) or receipt.step != expected or receipt.outcome != "passed" or not _https_url(receipt.evidence_url):
            return "invalid"
    if not _https_url(evidence.run_url) or not _https_url(evidence.evidence_url):
        return "invalid"
    revision = isinstance(evidence.source_revision, str) and re.fullmatch(r"[0-9a-f]{40}", evidence.source_revision)
    digest = isinstance(evidence.artifact_digest, str) and re.fullmatch(r"sha256:[0-9a-f]{64}", evidence.artifact_digest)
    if not revision and not digest:
        return "invalid"
    if evidence.source_revision and not revision or evidence.artifact_digest and not digest:
        return "invalid"
    if evidence.mode == "live":
        urls = (evidence.run_url, evidence.evidence_url, *(receipt.evidence_url for receipt in evidence.steps))
        if any(_fixture_host(url) for url in urls):
            return "invalid"
        if evidence.source_revision == "0" * 40 or evidence.artifact_digest == "sha256:" + "0" * 64:
            return "invalid"
    return f"{evidence.mode}_claim_requires_verification"


def _https_url(value: object) -> bool:
    if not isinstance(value, str) or not value or any(c.isspace() for c in value):
        return False
    try:
        parsed = urlsplit(value)
        return parsed.scheme == "https" and bool(parsed.hostname) and not parsed.username and not parsed.password and parsed.port in (None, 443)
    except ValueError:
        return False


def _fixture_host(url: str) -> bool:
    # Reserved fixture domains cannot serve as real run or evidence locations.
    hostname = (urlsplit(url).hostname or "").rstrip(".")
    return any(hostname == suffix or hostname.endswith("." + suffix) for suffix in ("invalid", "test", "example", "localhost"))

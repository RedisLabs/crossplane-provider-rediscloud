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


@dataclass(frozen=True)
class Scenario:
    id: str
    steps: tuple[str, ...]


@dataclass(frozen=True)
class Evidence:
    provider: str
    scenario_id: str
    mode: str  # live or simulated; never inferred from a test name
    source_revision: str
    artifact_digest: str
    run_url: str
    evidence_url: str
    steps: tuple[str, ...]
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
    if not isinstance(data, dict) or set(data) != {"version", "scenarios"} or type(data["version"]) is not int or data["version"] != 1:
        raise ValueError("unsupported scenario document")
    if not isinstance(data["scenarios"], list) or not data["scenarios"]:
        raise ValueError("scenario list is empty or malformed")
    result = {}
    for entry in data["scenarios"]:
        if not isinstance(entry, dict) or set(entry) != {"id", "steps"}:
            raise ValueError("malformed scenario")
        name, steps = entry["id"], entry["steps"]
        if not isinstance(name, str) or not re.fullmatch(r"[a-z0-9-]+", name):
            raise ValueError("invalid scenario id")
        if name in result or not isinstance(steps, list) or not steps:
            raise ValueError("duplicate scenario or malformed steps")
        if any(not isinstance(s, str) or not re.fullmatch(r"[a-z0-9_]+", s) for s in steps) or len(set(steps)) != len(steps):
            raise ValueError("invalid or duplicate step")
        if steps[0] != "admission_and_clean_baseline" or steps[-1] != "complete_clean_inventory":
            raise ValueError("scenario lacks safety boundaries")
        result[name] = Scenario(name, tuple(steps))
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
    if evidence.direct_cloud_recovery or evidence.cleanup != "clean" or evidence.steps != scenario.steps:
        return "invalid"
    if not all(isinstance(x, str) and x.strip() for x in (evidence.run_url, evidence.evidence_url)):
        return "invalid"
    if not any(isinstance(x, str) and x.strip() for x in (evidence.source_revision, evidence.artifact_digest)):
        return "invalid"
    return f"{evidence.mode}_claim_requires_verification"

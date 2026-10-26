#!/usr/bin/env python3
"""Open a pull request when Terraform Redis Cloud publishes a newer release.

The workflow updates TERRAFORM_PROVIDER_VERSION and regenerates. It does not
merge, and it does not change the Crossplane, Go, or Upjet versions.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import subprocess
import tempfile
import urllib.request
from pathlib import Path

UPSTREAM = "RedisLabs/terraform-provider-rediscloud"
GITHUB_REPO = "RedisLabs/crossplane-provider-rediscloud"
PIN_LINE = re.compile(
    r"(?m)^(export TERRAFORM_PROVIDER_VERSION \?= )(\d+\.\d+\.\d+)[ \t]*$"
)
REPO_NAME = re.compile(r"[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+")


def validate_repo(name: str) -> str:
    if not REPO_NAME.fullmatch(name):
        raise SystemExit(f"invalid GitHub repo: {name}")
    return name


def normalize_version(tag: str) -> str:
    text = tag.strip()
    if text[:1] in ("v", "V"):
        text = text[1:]
    parts = text.split(".")
    if len(parts) != 3:
        raise SystemExit(f"pre-release tags are not tracked: {tag}")
    for part in parts:
        if not part.isdigit() or (len(part) > 1 and part.startswith("0")):
            raise SystemExit(f"pre-release tags are not tracked: {tag}")
    return text


def version_tuple(version: str) -> tuple[int, int, int]:
    normalized = normalize_version(version)
    major, minor, patch = normalized.split(".")
    return int(major), int(minor), int(patch)


def read_pin(makefile: str) -> str:
    found = PIN_LINE.findall(makefile)
    if len(found) != 1:
        raise SystemExit(
            f"expected one TERRAFORM_PROVIDER_VERSION line, found {len(found)}"
        )
    return found[0][1]


def bump_pin(makefile: str, version: str) -> str:
    candidate = normalize_version(version)
    bumped, count = PIN_LINE.subn(rf"\g<1>{candidate}", makefile, count=1)
    if count != 1 or read_pin(bumped) != candidate:
        raise SystemExit("TERRAFORM_PROVIDER_VERSION line was not updated")
    return bumped


def assignment(text: str, name: str) -> str:
    found = re.findall(
        rf"(?m)^(?:export[ \t]+)?{re.escape(name)}[ \t]*(?:\?=|:=|=)[ \t]*(\S+)[ \t]*$",
        text,
    )
    if len(found) != 1:
        raise SystemExit(f"expected one {name} assignment, found {found}")
    return found[0]


def directive(text: str, name: str) -> str:
    found = re.findall(rf"(?m)^{re.escape(name)}[ \t]+(\S+)[ \t]*$", text)
    if len(found) != 1:
        raise SystemExit(f"expected one {name} directive, found {found}")
    return found[0]


def module_version(text: str, module: str) -> str:
    found = re.findall(
        rf"(?m)^[ \t]*{re.escape(module)}[ \t]+(v\S+)[ \t]*$",
        text,
    )
    if len(found) != 1:
        raise SystemExit(f"expected one require for {module}, found {found}")
    return found[0]


def workflow_env(text: str, name: str) -> str:
    found = re.findall(
        rf"(?m)^[ \t]*{re.escape(name)}:[ \t]*['\"]([^'\"]+)['\"][ \t]*$",
        text,
    )
    if len(found) != 1:
        raise SystemExit(f"expected one {name} workflow value, found {found}")
    return found[0]


def protected_snapshot(root: Path) -> dict[str, str]:
    makefile = (root / "Makefile").read_text()
    gomod = (root / "go.mod").read_text()
    ci = (root / ".github/workflows/ci.yml").read_text()
    snap = {
        "CROSSPLANE_VERSION": assignment(makefile, "CROSSPLANE_VERSION"),
        "TERRAFORM_VERSION": assignment(makefile, "TERRAFORM_VERSION"),
        "go": directive(gomod, "go"),
        "toolchain": directive(gomod, "toolchain"),
        "upjet": module_version(gomod, "github.com/crossplane/upjet"),
        "crossplane-runtime": module_version(
            gomod, "github.com/crossplane/crossplane-runtime"
        ),
        "ci.GO_VERSION": workflow_env(ci, "GO_VERSION"),
    }
    uptest = root / ".github/workflows/uptest-trigger.yaml"
    if uptest.exists():
        snap["uptest.GO_VERSION"] = workflow_env(uptest.read_text(), "GO_VERSION")
    return snap


def assert_timeouts(root: Path) -> int:
    files = sorted((root / "internal/controller").glob("**/zz_controller.go"))
    if not files:
        raise SystemExit("no generated controllers after generate")
    for path in files:
        text = path.read_text()
        if "3 * time.Minute" in text or "3*time.Minute" in text:
            raise SystemExit(f"controller timeout reverted: {path}")
        if "30 * time.Minute" not in text:
            raise SystemExit(f"missing 30 minute timeout: {path}")
    return len(files)


def fetch_latest_tag(upstream: str, opener=urllib.request.urlopen) -> str:
    repo = validate_repo(upstream)
    request = urllib.request.Request(
        f"https://api.github.com/repos/{repo}/releases/latest",
        headers={
            "Accept": "application/vnd.github+json",
            "User-Agent": "crossplane-provider-rediscloud",
            "X-GitHub-Api-Version": "2022-11-28",
        },
    )
    token = os.environ.get("GITHUB_TOKEN") or os.environ.get("GH_TOKEN")
    if token:
        request.add_header("Authorization", f"Bearer {token}")
    try:
        with opener(request, timeout=30) as response:
            payload = json.load(response)
    except Exception as exc:
        raise SystemExit(
            f"failed to read the latest Terraform provider release: {exc}"
        ) from exc
    if "tag_name" not in payload:
        raise SystemExit("latest Terraform provider release has no tag")
    return normalize_version(str(payload["tag_name"]))


def run(args: list[str], cwd: Path) -> str:
    completed = subprocess.run(
        args,
        cwd=cwd,
        text=True,
        capture_output=True,
        check=False,
    )
    if completed.returncode != 0:
        detail = completed.stderr.strip() or completed.stdout.strip()
        raise SystemExit(f"{' '.join(args)} failed: {detail}")
    return completed.stdout


def resolve_ref(root: Path, ref: str) -> str | None:
    completed = subprocess.run(
        ["git", "rev-parse", "--verify", ref],
        cwd=root,
        text=True,
        capture_output=True,
        check=False,
    )
    if completed.returncode != 0:
        return None
    return completed.stdout.strip()


def assert_head_is_main(root: Path) -> None:
    head = resolve_ref(root, "HEAD")
    main = resolve_ref(root, "origin/main") or resolve_ref(root, "main")
    if head is None or main is None or head != main:
        raise SystemExit("refusing to open a pull request unless HEAD is main")


def default_generate(root: Path) -> None:
    completed = subprocess.run(["make", "generate"], cwd=root, check=False)
    if completed.returncode != 0:
        raise SystemExit("make generate failed")


def write_temp(name: str, content: str) -> str:
    handle = tempfile.NamedTemporaryFile(
        "w",
        prefix=f"track-release-{name}-",
        suffix=".txt",
        delete=False,
    )
    with handle:
        handle.write(content)
    return handle.name


def open_pull_request(
    root: Path,
    candidate: str,
    *,
    github_repo: str,
    skip_generate: bool,
    generate_fn=default_generate,
) -> str:
    root = root.resolve()
    github_repo = validate_repo(github_repo)
    candidate = normalize_version(candidate)
    assert_head_is_main(root)
    if run(["git", "status", "--porcelain"], root).strip():
        raise SystemExit("working tree is not clean; refusing to open a pull request")

    branch = f"track/terraform-provider-{candidate}"
    existing = json.loads(
        run(
            [
                "gh",
                "pr",
                "list",
                "--repo",
                github_repo,
                "--head",
                branch,
                "--state",
                "open",
                "--json",
                "number",
            ],
            root,
        )
        or "[]"
    )
    if existing:
        print(f"decision=already-open pin={read_pin((root / 'Makefile').read_text())} candidate={candidate}")
        return "already-open"

    before = protected_snapshot(root)
    makefile_path = root / "Makefile"
    makefile_path.write_text(bump_pin(makefile_path.read_text(), candidate))
    if not skip_generate:
        generate_fn(root)
        assert_timeouts(root)
    after = protected_snapshot(root)
    if before != after:
        changed = {
            key: (before.get(key), after.get(key))
            for key in sorted(set(before) | set(after))
            if before.get(key) != after.get(key)
        }
        raise SystemExit(
            "refusing to open a pull request; protected versions changed: "
            f"{changed}"
        )
    if read_pin(makefile_path.read_text()) != candidate:
        raise SystemExit("pin was not updated to the candidate release")

    run(["git", "checkout", "-B", branch], root)
    message = write_temp(
        "commit",
        "\n".join(
            [
                f"Bump the Terraform Redis Cloud provider to v{candidate}.",
                "",
                "The release-tracking workflow updates TERRAFORM_PROVIDER_VERSION and regenerates from that release. It does not merge.",
                "",
            ]
        ),
    )
    body = write_temp(
        "body",
        "\n".join(
            [
                "This pull request was opened by `.github/workflows/terraform-release.yaml`.",
                "",
                f"`RedisLabs/terraform-provider-rediscloud` published v{candidate}, which is newer than `TERRAFORM_PROVIDER_VERSION` on main. This changes that pin and regenerates the provider. It does not change the Crossplane, Go, or Upjet versions. It does not merge itself.",
                "",
                "Review the generated diff and confirm the controller timeout is still 30 minutes. Merge only after CI is green. A pull request opened with the workflow token does not start GitHub Actions by itself. Push a commit from a user account if checks do not appear.",
                "",
            ]
        ),
    )
    try:
        run(["git", "add", "-A"], root)
        run(
            [
                "git",
                "-c",
                "user.email=41898282+github-actions[bot]@users.noreply.github.com",
                "-c",
                "user.name=github-actions[bot]",
                "-c",
                "commit.gpgsign=false",
                "commit",
                "-F",
                message,
            ],
            root,
        )
        run(["git", "push", "--set-upstream", "origin", branch], root)
        url = run(
            [
                "gh",
                "pr",
                "create",
                "--repo",
                github_repo,
                "--base",
                "main",
                "--head",
                branch,
                "--title",
                f"Bump Terraform provider to v{candidate}",
                "--body-file",
                body,
            ],
            root,
        ).strip()
    finally:
        os.unlink(message)
        os.unlink(body)
    print(f"decision=opened candidate={candidate} url={url}")
    return url


def candidate_for(pin: str, tag: str, upstream: str) -> str:
    if tag.strip():
        return normalize_version(tag)
    return fetch_latest_tag(upstream)


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repo-root", default=".")
    parser.add_argument("--upstream", default=UPSTREAM)
    parser.add_argument(
        "--github-repo",
        default=os.environ.get("GITHUB_REPOSITORY", GITHUB_REPO),
    )
    parser.add_argument("--tag", default="")
    parser.add_argument("--dry-run", action="store_true")
    parser.add_argument(
        "--skip-generate",
        action="store_true",
        help="Test-only. Skip make generate and the controller timeout check.",
    )
    args = parser.parse_args(argv)
    if not args.tag:
        args.tag = os.environ.get("TRACK_TAG", "")
    if os.environ.get("TRACK_DRY_RUN", "").lower() == "true":
        args.dry_run = True

    root = Path(args.repo_root).resolve()
    pin = read_pin((root / "Makefile").read_text())
    candidate = candidate_for(pin, args.tag, args.upstream)
    newer = version_tuple(candidate) > version_tuple(pin)
    if args.dry_run or not newer:
        label = "would-open" if newer else "none"
        print(f"decision={label} pin={pin} candidate={candidate}")
        return 0

    open_pull_request(
        root,
        candidate,
        github_repo=args.github_repo,
        skip_generate=args.skip_generate,
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

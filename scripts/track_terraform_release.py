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


TOKEN_ENV_NAMES = ("GH_TOKEN", "GITHUB_TOKEN", "GITHUB_ENTERPRISE_TOKEN")


def scrubbed_environ() -> dict[str, str]:
    env = dict(os.environ)
    for name in TOKEN_ENV_NAMES:
        env.pop(name, None)
    return env


def without_github_tokens() -> dict[str, str]:
    saved: dict[str, str] = {}
    for name in TOKEN_ENV_NAMES:
        if name in os.environ:
            saved[name] = os.environ.pop(name)
    return saved


def restore_github_tokens(saved: dict[str, str]) -> None:
    os.environ.update(saved)


def default_generate(root: Path) -> None:
    completed = subprocess.run(
        ["make", "generate"],
        cwd=root,
        check=False,
        env=scrubbed_environ(),
    )
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


def branch_name(candidate: str) -> str:
    return f"track/terraform-provider-{normalize_version(candidate)}"


def github_output(values: dict[str, str]) -> None:
    path = os.environ.get("GITHUB_OUTPUT")
    if not path:
        return
    with open(path, "a", encoding="utf-8") as handle:
        for key, value in values.items():
            if "\n" in value or "\r" in value:
                raise SystemExit(f"refusing multiline GitHub output for {key}")
            handle.write(f"{key}={value}\n")


def bundle_destination() -> Path:
    directory = Path(os.environ.get("RUNNER_TEMP", tempfile.gettempdir()))
    directory.mkdir(parents=True, exist_ok=True)
    return directory / "track-release.bundle"


def remote_branch_sha(root: Path, branch: str) -> str | None:
    completed = subprocess.run(
        ["git", "ls-remote", "origin", f"refs/heads/{branch}"],
        cwd=root,
        text=True,
        capture_output=True,
        check=False,
    )
    if completed.returncode != 0:
        detail = completed.stderr.strip() or completed.stdout.strip()
        raise SystemExit(f"git ls-remote failed: {detail}")
    line = completed.stdout.strip()
    if not line:
        return None
    sha = line.split()[0]
    if len(sha) < 7:
        raise SystemExit(f"unexpected ls-remote output: {line}")
    return sha


def pin_at(root: Path, ref: str) -> str:
    return read_pin(run(["git", "show", f"{ref}:Makefile"], root))


def existing_pull_request(root: Path, github_repo: str, branch: str) -> bool:
    listed = json.loads(
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
    return bool(listed)


def commit_message(candidate: str) -> str:
    return "\n".join(
        [
            f"Bump the Terraform Redis Cloud provider to v{candidate}.",
            "",
            "The release-tracking workflow updates TERRAFORM_PROVIDER_VERSION and regenerates from that release. It does not merge.",
            "",
        ]
    )


def pull_request_body(candidate: str) -> str:
    return "\n".join(
        [
            "This pull request was opened by `.github/workflows/terraform-release.yaml`.",
            "",
            f"`RedisLabs/terraform-provider-rediscloud` published v{candidate}, which is newer than `TERRAFORM_PROVIDER_VERSION` on main. This changes that pin and regenerates the provider. It does not change the Crossplane, Go, or Upjet versions. It does not merge itself.",
            "",
            "Review the generated diff and confirm the controller timeout is still 30 minutes. Merge only after CI is green. A pull request opened with the workflow token does not start GitHub Actions by itself. Push a commit from a user account if checks do not appear.",
            "",
            "Generation runs without the token that pushes and opens pull requests. If the branch was pushed and opening the pull request failed, the next run opens the pull request from that branch.",
            "",
        ]
    )


def create_pull_request(root: Path, github_repo: str, branch: str, candidate: str) -> str:
    body = write_temp("body", pull_request_body(candidate))
    try:
        return run(
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
        os.unlink(body)


def push_branch(root: Path, branch: str, remote_sha: str | None) -> None:
    if remote_sha is None:
        run(["git", "push", "--set-upstream", "origin", branch], root)
        return
    run(
        [
            "git",
            "push",
            "origin",
            f"{branch}:{branch}",
            f"--force-with-lease=refs/heads/{branch}:{remote_sha}",
        ],
        root,
    )


def require_clean_main(root: Path) -> None:
    assert_head_is_main(root)
    if run(["git", "status", "--porcelain"], root).strip():
        raise SystemExit("working tree is not clean; refusing to open a pull request")


def generate_and_commit(
    root: Path,
    candidate: str,
    *,
    skip_generate: bool,
    generate_fn=default_generate,
) -> str:
    branch = branch_name(candidate)
    before = protected_snapshot(root)
    makefile_path = root / "Makefile"
    makefile_path.write_text(bump_pin(makefile_path.read_text(), candidate))
    saved_tokens = without_github_tokens()
    try:
        if not skip_generate:
            generate_fn(root)
            assert_timeouts(root)
    finally:
        restore_github_tokens(saved_tokens)
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
    message = write_temp("commit", commit_message(candidate))
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
    finally:
        os.unlink(message)
    return branch


def remote_candidate_sha(root: Path, branch: str, candidate: str) -> str | None:
    remote_sha = remote_branch_sha(root, branch)
    if remote_sha is None:
        return None
    run(["git", "fetch", "origin", f"refs/heads/{branch}"], root)
    if pin_at(root, "FETCH_HEAD") != candidate:
        return None
    return remote_sha


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
    require_clean_main(root)
    branch = branch_name(candidate)
    if existing_pull_request(root, github_repo, branch):
        print(
            f"decision=already-open pin={read_pin((root / 'Makefile').read_text())} candidate={candidate}"
        )
        return "already-open"

    remote_sha = remote_branch_sha(root, branch)
    if remote_candidate_sha(root, branch, candidate) is not None:
        url = create_pull_request(root, github_repo, branch, candidate)
        print(f"decision=retried candidate={candidate} url={url}")
        return url

    generate_and_commit(
        root,
        candidate,
        skip_generate=skip_generate,
        generate_fn=generate_fn,
    )
    push_branch(root, branch, remote_sha)
    url = create_pull_request(root, github_repo, branch, candidate)
    print(f"decision=opened candidate={candidate} url={url}")
    return url


def prepare_release(
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
    require_clean_main(root)
    branch = branch_name(candidate)
    if existing_pull_request(root, github_repo, branch):
        print(
            f"decision=already-open pin={read_pin((root / 'Makefile').read_text())} candidate={candidate}"
        )
        github_output({"action": "already-open", "candidate": candidate, "branch": branch})
        return "already-open"
    if remote_candidate_sha(root, branch, candidate) is not None:
        print(f"decision=retry candidate={candidate}")
        github_output({"action": "retry", "candidate": candidate, "branch": branch})
        return "retry"

    generate_and_commit(
        root,
        candidate,
        skip_generate=skip_generate,
        generate_fn=generate_fn,
    )
    destination = bundle_destination()
    if destination.exists():
        destination.unlink()
    run(["git", "bundle", "create", str(destination), branch], root)
    print(f"decision=prepared candidate={candidate} bundle={destination}")
    github_output(
        {
            "action": "publish",
            "candidate": candidate,
            "branch": branch,
            "bundle": str(destination),
        }
    )
    return "publish"


def publish_release(
    root: Path,
    candidate: str,
    *,
    github_repo: str,
    reuse: bool,
    bundle: Path | None,
) -> str:
    root = root.resolve()
    github_repo = validate_repo(github_repo)
    candidate = normalize_version(candidate)
    require_clean_main(root)
    branch = branch_name(candidate)
    if existing_pull_request(root, github_repo, branch):
        print(
            f"decision=already-open pin={read_pin((root / 'Makefile').read_text())} candidate={candidate}"
        )
        return "already-open"
    if reuse:
        if remote_candidate_sha(root, branch, candidate) is None:
            raise SystemExit("remote branch is missing or does not carry the candidate pin")
        url = create_pull_request(root, github_repo, branch, candidate)
        print(f"decision=retried candidate={candidate} url={url}")
        return url
    if bundle is None or not bundle.is_file():
        raise SystemExit("release bundle is missing")
    run(
        ["git", "fetch", str(bundle), f"refs/heads/{branch}:refs/heads/{branch}"],
        root,
    )
    remote_sha = remote_branch_sha(root, branch)
    local_sha = resolve_ref(root, f"refs/heads/{branch}")
    if local_sha is None:
        raise SystemExit("bundle did not create the release branch")
    if remote_sha != local_sha:
        push_branch(root, branch, remote_sha)
    url = create_pull_request(root, github_repo, branch, candidate)
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
        "--prepare-only",
        action="store_true",
        help="Bump, generate, and bundle a commit. Do not push or open a pull request.",
    )
    parser.add_argument(
        "--publish-only",
        action="store_true",
        help="Push a prepared bundle, or open a pull request for an existing branch.",
    )
    parser.add_argument("--reuse-remote", action="store_true")
    parser.add_argument("--bundle", default="")
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
    if args.publish_only:
        raw_candidate = args.tag or os.environ.get("TRACK_CANDIDATE", "")
        if not raw_candidate:
            raise SystemExit("publish-only requires a candidate")
        candidate = normalize_version(raw_candidate)
        reuse = args.reuse_remote or os.environ.get("TRACK_REUSE", "").lower() == "true"
        bundle_text = args.bundle or os.environ.get("TRACK_BUNDLE", "")
        publish_release(
            root,
            candidate,
            github_repo=args.github_repo,
            reuse=reuse,
            bundle=Path(bundle_text) if bundle_text else None,
        )
        return 0

    pin = read_pin((root / "Makefile").read_text())
    candidate = candidate_for(pin, args.tag, args.upstream)
    newer = version_tuple(candidate) > version_tuple(pin)
    if args.dry_run or not newer:
        label = "would-open" if newer else "none"
        print(f"decision={label} pin={pin} candidate={candidate}")
        github_output(
            {
                "action": "none",
                "candidate": candidate,
                "branch": branch_name(candidate),
            }
        )
        return 0

    if args.prepare_only:
        prepare_release(
            root,
            candidate,
            github_repo=args.github_repo,
            skip_generate=args.skip_generate,
        )
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

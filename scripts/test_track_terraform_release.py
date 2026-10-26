#!/usr/bin/env python3
"""Tests for the Terraform provider release-tracking workflow."""

from __future__ import annotations

import io
import json
import os
import subprocess
import tempfile
import unittest
from contextlib import contextmanager, redirect_stdout
from pathlib import Path

import track_terraform_release as track

REPO = Path(__file__).resolve().parents[1]


def git(repo: Path, *args: str, check: bool = True) -> subprocess.CompletedProcess[str]:
    return subprocess.run(
        ["git", "-C", str(repo), *args],
        text=True,
        capture_output=True,
        check=check,
    )


def write_fixture_files(repo: Path, pin: str = "2.19.2") -> None:
    (repo / "Makefile").write_text(
        "\n".join(
            [
                f"export TERRAFORM_PROVIDER_VERSION ?= {pin}",
                "export TERRAFORM_VERSION ?= 1.5.7",
                "CROSSPLANE_VERSION = 1.16.0",
                "",
            ]
        )
    )
    (repo / "go.mod").write_text(
        "\n".join(
            [
                "module github.com/RedisLabs/provider-rediscloud",
                "",
                "go 1.24.0",
                "",
                "toolchain go1.24.11",
                "",
                "require (",
                "\tgithub.com/crossplane/crossplane-runtime v1.16.0",
                "\tgithub.com/crossplane/upjet v1.4.1",
                ")",
                "",
            ]
        )
    )
    workflow = repo / ".github/workflows"
    workflow.mkdir(parents=True)
    (workflow / "ci.yml").write_text("env:\n  GO_VERSION: '1.24'\n")


def init_repo(repo: Path) -> None:
    git(repo, "init", "-b", "main")
    git(repo, "config", "user.email", "test@example.com")
    git(repo, "config", "user.name", "Test")
    git(repo, "config", "commit.gpgsign", "false")
    write_fixture_files(repo)
    git(repo, "add", "-A")
    git(repo, "commit", "-m", "initial")
    bare = repo.parent / "origin.git"
    subprocess.run(["git", "init", "--bare", "-b", "main", str(bare)], check=True, capture_output=True)
    git(repo, "remote", "add", "origin", str(bare))
    git(repo, "push", "origin", "main")


class GhLog:
    def __init__(self) -> None:
        self.path: Path | None = None
        self._calls: list[dict] = []

    @property
    def calls(self) -> list[dict]:
        if self.path is not None and self.path.exists():
            return gh_calls(self.path)
        return self._calls


@contextmanager
def fake_gh(list_json: str = "[]"):
    holder = GhLog()
    with tempfile.TemporaryDirectory() as tmp:
        root = Path(tmp)
        log = root / "gh.log"
        script = root / "gh"
        script.write_text(
            "\n".join(
                [
                    "#!/usr/bin/env python3",
                    "import json, os, sys",
                    "record = {'argv': sys.argv[1:]}",
                    "if '--body-file' in sys.argv:",
                    "    body_path = sys.argv[sys.argv.index('--body-file') + 1]",
                    "    record['body'] = open(body_path, encoding='utf-8').read()",
                    "with open(os.environ['GH_LOG'], 'a', encoding='utf-8') as handle:",
                    "    json.dump(record, handle)",
                    "    handle.write('\\n')",
                    "if sys.argv[1:3] == ['pr', 'list']:",
                    "    sys.stdout.write(os.environ.get('GH_LIST', '[]'))",
                    "    raise SystemExit(0)",
                    "if sys.argv[1:3] == ['pr', 'create']:",
                    "    if 'merge' in sys.argv:",
                    "        raise SystemExit(2)",
                    "    sys.stdout.write('https://example.test/pull/1\\n')",
                    "    raise SystemExit(0)",
                    "sys.stderr.write('unexpected gh command\\n')",
                    "raise SystemExit(1)",
                    "",
                ]
            )
        )
        script.chmod(0o755)
        old_path = os.environ.get("PATH", "")
        old_log = os.environ.get("GH_LOG")
        old_list = os.environ.get("GH_LIST")
        os.environ["PATH"] = str(root) + os.pathsep + old_path
        os.environ["GH_LOG"] = str(log)
        os.environ["GH_LIST"] = list_json
        holder.path = log
        try:
            yield holder
        finally:
            holder._calls = gh_calls(log)
            holder.path = None
            os.environ["PATH"] = old_path
            if old_log is None:
                os.environ.pop("GH_LOG", None)
            else:
                os.environ["GH_LOG"] = old_log
            if old_list is None:
                os.environ.pop("GH_LIST", None)
            else:
                os.environ["GH_LIST"] = old_list


def gh_calls(log: Path) -> list[dict]:
    if not log.exists():
        return []
    return [json.loads(line) for line in log.read_text().splitlines() if line]


class TrackReleaseTest(unittest.TestCase):
    def test_real_makefile_pin_is_current_release(self) -> None:
        self.assertEqual(track.read_pin((REPO / "Makefile").read_text()), "2.19.2")

    def test_bump_real_makefile_changes_only_the_pin(self) -> None:
        original = (REPO / "Makefile").read_text()
        bumped = track.bump_pin(original, "v2.20.0")
        self.assertEqual((REPO / "Makefile").read_text(), original)
        self.assertEqual(track.read_pin(bumped), "2.20.0")
        self.assertEqual(
            track.assignment(original, "CROSSPLANE_VERSION"),
            track.assignment(bumped, "CROSSPLANE_VERSION"),
        )
        self.assertEqual(
            track.assignment(original, "TERRAFORM_VERSION"),
            track.assignment(bumped, "TERRAFORM_VERSION"),
        )
        changed = [
            (old, new)
            for old, new in zip(original.splitlines(), bumped.splitlines())
            if old != new
        ]
        self.assertEqual(
            changed,
            [
                (
                    "export TERRAFORM_PROVIDER_VERSION ?= 2.19.2",
                    "export TERRAFORM_PROVIDER_VERSION ?= 2.20.0",
                )
            ],
        )

    def test_version_order_and_prerelease_rejection(self) -> None:
        self.assertFalse(track.version_tuple("2.19.2") > track.version_tuple("2.19.2"))
        self.assertFalse(track.version_tuple("v2.19.1") > track.version_tuple("2.19.2"))
        self.assertTrue(track.version_tuple("2.19.3") > track.version_tuple("2.19.2"))
        self.assertTrue(track.version_tuple("v2.20.0") > track.version_tuple("2.19.2"))
        with self.assertRaises(SystemExit) as caught:
            track.normalize_version("v2.19.3-rc.1")
        self.assertIn("pre-release", str(caught.exception))

    def test_fetch_latest_reads_tag_name(self) -> None:
        class Response:
            def __enter__(self):
                return self

            def __exit__(self, *_args):
                return False

            def read(self):
                return b'{"tag_name":"v2.19.2"}'

        def opener(request, timeout):
            self.assertEqual(timeout, 30)
            self.assertIn(
                "/repos/RedisLabs/terraform-provider-rediscloud/releases/latest",
                request.full_url,
            )
            return Response()

        self.assertEqual(
            track.fetch_latest_tag("RedisLabs/terraform-provider-rediscloud", opener),
            "2.19.2",
        )
        with self.assertRaises(SystemExit):
            track.fetch_latest_tag("not a repo", opener)

    def test_protected_snapshot_reads_current_tree(self) -> None:
        snap = track.protected_snapshot(REPO)
        self.assertEqual(snap["CROSSPLANE_VERSION"], "1.16.0")
        self.assertEqual(snap["TERRAFORM_VERSION"], "1.5.7")
        self.assertEqual(snap["go"], "1.24.0")
        self.assertEqual(snap["toolchain"], "go1.24.11")
        self.assertEqual(snap["upjet"], "v1.4.1")
        self.assertEqual(snap["crossplane-runtime"], "v1.16.0")
        self.assertEqual(snap["ci.GO_VERSION"], "1.24")
        self.assertEqual(snap["uptest.GO_VERSION"], "1.21")

    def test_current_controllers_keep_thirty_minute_timeout(self) -> None:
        self.assertGreaterEqual(track.assert_timeouts(REPO), 27)

    def test_timeout_guard_rejects_three_minutes(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            path = root / "internal/controller/example/example/zz_controller.go"
            path.parent.mkdir(parents=True)
            path.write_text("managed.WithTimeout(3 * time.Minute)\n")
            with self.assertRaises(SystemExit):
                track.assert_timeouts(root)

    def test_dry_run_same_tag_opens_nothing(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_fixture_files(root)
            before = (root / "Makefile").read_text()
            output = io.StringIO()
            with redirect_stdout(output):
                code = track.main(
                    ["--dry-run", "--tag", "v2.19.2", "--repo-root", str(root)]
                )
            self.assertEqual(code, 0)
            self.assertIn("decision=none pin=2.19.2 candidate=2.19.2", output.getvalue())
            self.assertEqual((root / "Makefile").read_text(), before)

    def test_dry_run_newer_tag_does_not_edit_or_open(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            write_fixture_files(root)
            before = (root / "Makefile").read_text()
            output = io.StringIO()
            with fake_gh() as log, redirect_stdout(output):
                code = track.main(
                    ["--dry-run", "--tag", "v2.20.0", "--repo-root", str(root)]
                )
            self.assertEqual(code, 0)
            self.assertIn(
                "decision=would-open pin=2.19.2 candidate=2.20.0",
                output.getvalue(),
            )
            self.assertEqual((root / "Makefile").read_text(), before)
            self.assertEqual(log.calls, [])

    def test_fixture_tag_opens_pull_request_and_bumps_only_the_pin(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp) / "repo"
            repo.mkdir()
            init_repo(repo)
            output = io.StringIO()
            with fake_gh() as log, redirect_stdout(output):
                code = track.main(
                    [
                        "--tag",
                        "v2.20.0",
                        "--skip-generate",
                        "--repo-root",
                        str(repo),
                        "--github-repo",
                        "local/test",
                    ]
                )
            self.assertEqual(code, 0)
            self.assertIn("decision=opened candidate=2.20.0", output.getvalue())
            self.assertIn("https://example.test/pull/1", output.getvalue())
            makefile = (repo / "Makefile").read_text()
            self.assertIn("export TERRAFORM_PROVIDER_VERSION ?= 2.20.0", makefile)
            self.assertIn("CROSSPLANE_VERSION = 1.16.0", makefile)
            self.assertIn("export TERRAFORM_VERSION ?= 1.5.7", makefile)
            show = git(repo, "show", "-1", "--", "Makefile").stdout
            self.assertIn("TERRAFORM_PROVIDER_VERSION ?= 2.20.0", show)
            changed_crossplane = [
                line
                for line in show.splitlines()
                if line.startswith(("+", "-")) and "CROSSPLANE" in line
            ]
            self.assertEqual(changed_crossplane, [])
            calls = log.calls
            self.assertEqual(calls[0]["argv"][:2], ["pr", "list"])
            create = calls[1]["argv"]
            self.assertEqual(create[:2], ["pr", "create"])
            self.assertNotIn("merge", create)
            self.assertIn("Bump Terraform provider to v2.20.0", create)
            self.assertIn("does not merge", calls[1]["body"])
            self.assertIn("TERRAFORM_PROVIDER_VERSION", calls[1]["body"])

    def test_generate_must_keep_timeout_before_a_pull_request(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp) / "repo"
            repo.mkdir()
            init_repo(repo)
            seen: list[Path] = []

            def generate(root: Path) -> None:
                seen.append(root)
                path = root / "internal/controller/example/example/zz_controller.go"
                path.parent.mkdir(parents=True)
                path.write_text("managed.WithTimeout(30 * time.Minute)\n")

            with fake_gh() as log, redirect_stdout(io.StringIO()):
                track.open_pull_request(
                    repo,
                    "2.20.0",
                    github_repo="local/test",
                    skip_generate=False,
                    generate_fn=generate,
                )
                self.assertEqual(seen, [repo.resolve()])
                self.assertEqual(log.calls[1]["argv"][:2], ["pr", "create"])
            self.assertIn("30 * time.Minute", (repo / "internal/controller/example/example/zz_controller.go").read_text())

    def test_reverted_timeout_does_not_open_a_pull_request(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp) / "repo"
            repo.mkdir()
            init_repo(repo)

            def generate(root: Path) -> None:
                path = root / "internal/controller/example/example/zz_controller.go"
                path.parent.mkdir(parents=True)
                path.write_text("managed.WithTimeout(3 * time.Minute)\n")

            with fake_gh() as log:
                with self.assertRaises(SystemExit) as caught:
                    track.open_pull_request(
                        repo,
                        "2.20.0",
                        github_repo="local/test",
                        skip_generate=False,
                        generate_fn=generate,
                    )
                self.assertIn("controller timeout reverted", str(caught.exception))
                self.assertEqual([call["argv"][:2] for call in log.calls], [["pr", "list"]])
            self.assertEqual(git(repo, "rev-list", "--count", "HEAD").stdout.strip(), "1")

    def test_protected_version_change_does_not_open_a_pull_request(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp) / "repo"
            repo.mkdir()
            init_repo(repo)

            def generate(root: Path) -> None:
                path = root / "Makefile"
                path.write_text(
                    path.read_text().replace(
                        "CROSSPLANE_VERSION = 1.16.0",
                        "CROSSPLANE_VERSION = 2.4.2",
                    )
                )
                controller = root / "internal/controller/example/example/zz_controller.go"
                controller.parent.mkdir(parents=True)
                controller.write_text("managed.WithTimeout(30 * time.Minute)\n")

            with fake_gh() as log:
                with self.assertRaises(SystemExit) as caught:
                    track.open_pull_request(
                        repo,
                        "2.20.0",
                        github_repo="local/test",
                        skip_generate=False,
                        generate_fn=generate,
                    )
                self.assertIn("protected versions changed", str(caught.exception))
                self.assertIn("CROSSPLANE_VERSION", str(caught.exception))
                self.assertEqual([call["argv"][:2] for call in log.calls], [["pr", "list"]])
            self.assertEqual(git(repo, "rev-list", "--count", "HEAD").stdout.strip(), "1")

    def test_existing_pull_request_is_left_alone(self) -> None:
        with tempfile.TemporaryDirectory() as tmp:
            repo = Path(tmp) / "repo"
            repo.mkdir()
            init_repo(repo)
            output = io.StringIO()
            with fake_gh('[{"number": 4}]') as log, redirect_stdout(output):
                result = track.open_pull_request(
                    repo,
                    "2.20.0",
                    github_repo="local/test",
                    skip_generate=True,
                )
                self.assertEqual(result, "already-open")
                self.assertIn("decision=already-open", output.getvalue())
                self.assertEqual([call["argv"][:2] for call in log.calls], [["pr", "list"]])
            self.assertIn("export TERRAFORM_PROVIDER_VERSION ?= 2.19.2", (repo / "Makefile").read_text())
            self.assertEqual(git(repo, "rev-list", "--count", "HEAD").stdout.strip(), "1")

    def test_live_dry_run_against_published_release_opens_nothing(self) -> None:
        before = (REPO / "Makefile").read_bytes()
        output = io.StringIO()
        with redirect_stdout(output):
            code = track.main(["--dry-run", "--repo-root", str(REPO)])
        self.assertEqual(code, 0)
        self.assertEqual((REPO / "Makefile").read_bytes(), before)
        self.assertIn("decision=none pin=2.19.2 candidate=2.19.2", output.getvalue())

    def test_workflow_tracks_releases_and_does_not_merge(self) -> None:
        workflow = (REPO / ".github/workflows/terraform-release.yaml").read_text()
        self.assertIn("scripts/track_terraform_release.py", workflow)
        self.assertIn("workflow_dispatch", workflow)
        self.assertIn("schedule:", workflow)
        self.assertIn("contents: write", workflow)
        self.assertIn("pull-requests: write", workflow)
        self.assertIn("dry_run", workflow)
        self.assertIn("timeout-minutes: 90", workflow)
        self.assertNotIn("pr merge", workflow)
        self.assertNotIn("CROSSPLANE_VERSION", workflow)


if __name__ == "__main__":
    unittest.main()

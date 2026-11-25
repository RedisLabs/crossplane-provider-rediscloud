import json
import unittest
from unittest.mock import patch

from go_test_receipts import MODULE, run_selected, validate_events

PACKAGE = "./scripts/qa_tfplan"
NAME = "TestExample"


def stream(*events):
    package = MODULE + "/scripts/qa_tfplan"
    return "\n".join(json.dumps({"Package": package, "Action": action, **({"Test": name} if name else {})}) for action, name in events)


class TestGoTestReceipts(unittest.TestCase):
    def test_pass_requires_run_and_pass_for_expected_test_and_package(self):
        validate_events(stream(("run", NAME), ("pass", NAME), ("pass", "")), PACKAGE, (NAME,))

    def test_no_match_skip_missing_package_and_wrong_test_are_rejected(self):
        cases = [
            stream(("pass", "")),
            stream(("run", NAME), ("skip", NAME), ("pass", "")),
            stream(("run", NAME), ("pass", NAME)),
            stream(("run", "TestOther"), ("pass", "TestOther"), ("pass", "")),
            stream(("run", NAME), ("run", NAME + "/sub"), ("skip", NAME + "/sub"), ("pass", NAME), ("pass", "")),
            "not-json",
        ]
        for case in cases:
            with self.subTest(case=case), self.assertRaises(ValueError):
                validate_events(case, PACKAGE, (NAME,))

    def test_multiple_expected_tests_cannot_pass_partially(self):
        with self.assertRaises(ValueError):
            validate_events(stream(("run", NAME), ("pass", NAME), ("pass", "")), PACKAGE, (NAME, "TestMissing"))

    def test_missing_expected_subtest_is_rejected(self):
        case = stream(("run", NAME), ("run", NAME + "/lifecycle"), ("pass", NAME + "/lifecycle"), ("pass", NAME), ("pass", ""))
        validate_events(case, PACKAGE, (NAME, NAME + "/lifecycle"))
        with self.assertRaises(ValueError):
            validate_events(case, PACKAGE, (NAME, NAME + "/lifecycle", NAME + "/accepted_then_503"))

    @patch.dict("os.environ", {}, clear=True)
    def test_not_hosted_or_missing_pinned_fixture_never_launches(self):
        with patch("go_test_receipts.subprocess.run") as run:
            with self.assertRaises(ValueError):
                run_selected(PACKAGE, (NAME,), require_fixture=True)
            with patch.dict("os.environ", {"GITHUB_ACTIONS": "true"}):
                with self.assertRaises(ValueError):
                    run_selected(PACKAGE, (NAME,), require_fixture=True)
            run.assert_not_called()

"""Synthetic tests for exact fixture receipt validation."""
import json
import unittest
from run_case import PACKAGE, ROOT, verify


def events(*items):
    return '\n'.join(json.dumps({'Package': PACKAGE, 'Test': name, 'Action': action}) for name, action in items)


class VerifyReceipt(unittest.TestCase):
    def test_exact_only(self):
        case = ROOT + '/lifecycle'
        good = events((ROOT, 'run'), (case, 'run'), (case, 'pass'), (ROOT, 'pass'))
        verify(good, 0, 'lifecycle')
        for bad, status in (
            (events((ROOT, 'run'), (ROOT, 'pass')), 0),
            (events((ROOT, 'run'), (case, 'run'), (case, 'skip'), (ROOT, 'pass')), 0),
            (good + '\n' + events((ROOT + '/source_cidrs', 'run')), 0),
            (good, 1),
            ('not-json', 0),
        ):
            with self.subTest(bad=bad, status=status), self.assertRaises(ValueError):
                verify(bad, status, 'lifecycle')

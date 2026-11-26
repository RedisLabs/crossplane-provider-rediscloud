import unittest
from unittest.mock import patch

import crossplane_alpha_lookup
import crossplane_alpha_regions


class TestLookupExecutionEvidence(unittest.TestCase):
    def test_missing_selected_test_produces_no_receipt(self):
        for module in (crossplane_alpha_lookup, crossplane_alpha_regions):
            with self.subTest(module=module.__name__):
                with patch.object(module.subprocess, "check_output", side_effect=["revision", ""]):
                    with patch.object(module, "run_selected", side_effect=ValueError("selected test did not run")):
                        with self.assertRaisesRegex(ValueError, "did not run"):
                            module.execute("revision")

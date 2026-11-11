"""Wait for an explicit screenshot acknowledgment committed by the verifier.

Timeout fails the run and permits the workflow's always-cleanup fallback.
The verifier writes acknowledgment only after all active screenshots are saved.
"""
import json
import os
import subprocess
import time

run = os.environ['GITHUB_RUN_ID'] + '-' + os.environ['GITHUB_RUN_ATTEMPT']
repo = os.environ['GITHUB_REPOSITORY']
endpoint = f'repos/{repo}/contents/ack/{run}.json?ref=codex/red225344-evidence-ack'
deadline = time.monotonic() + 1800
while time.monotonic() < deadline:
    p = subprocess.run(['gh','api',endpoint,'-H','Accept: application/vnd.github.raw+json'],capture_output=True,text=True)
    if p.returncode == 0:
        try:
            ack = json.loads(p.stdout)
            if ack.get('run') == run and ack.get('subscription_screenshot_sha256') and ack.get('database_screenshot_sha256'):
                print('Active SM evidence acknowledged; normal cleanup may begin')
                break
        except (ValueError,TypeError):
            pass
    time.sleep(20)
else:
    raise SystemExit('UI evidence acknowledgment timed out; acceptance incomplete; cleanup fallback required')

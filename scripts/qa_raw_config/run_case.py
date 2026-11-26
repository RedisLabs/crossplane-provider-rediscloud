"""Run exactly one named hosted fixture and reject all other subtests."""
import json
import os
import subprocess
import sys

ROOT = 'TestProDatabaseManagedLifecycleLoopback'
PACKAGE = 'github.com/RedisLabs/provider-rediscloud/scripts/qa_tfplan'
CASES = {'lifecycle', 'source_cidrs', 'policy_transition'}


def verify(output, returncode, selected):
    if selected not in CASES:
        raise ValueError('unexpected fixture case')
    try:
        events = [json.loads(line) for line in output.splitlines()]
    except json.JSONDecodeError as exc:
        raise ValueError('malformed Go test events') from exc
    if any(event.get('Package') != PACKAGE for event in events):
        raise ValueError('unexpected Go test package')
    case = ROOT + '/' + selected
    allowed = {ROOT, case}
    if any(event.get('Test') and event['Test'] not in allowed for event in events):
        raise ValueError('unexpected test or subtest executed')
    for name in (ROOT, case):
        actions = [event.get('Action') for event in events if event.get('Test') == name
                   and event.get('Action') in ('run', 'pass', 'fail', 'skip')]
        if actions != ['run', 'pass']:
            raise ValueError('expected selected test did not pass')
    if returncode != 0:
        raise ValueError('Go test process failed')


def main():
    if os.environ.get('GITHUB_ACTIONS') != 'true' or len(sys.argv) != 2 or sys.argv[1] not in CASES:
        raise ValueError('hosted named fixture required')
    case = sys.argv[1]
    env = os.environ.copy()
    env['QA_POLICY_TRANSITION'] = 'true' if case == 'policy_transition' else 'false'
    result = subprocess.run(
        ['go', 'test', '-json', './scripts/qa_tfplan', '-count=1', '-timeout=20m',
         '-run', '^' + ROOT + '$/^' + case + '$'],
        capture_output=True, text=True, timeout=1300, check=False, env=env,
    )
    try:
        verify(result.stdout, result.returncode, case)
    except ValueError:
        # These are fake-only fixture logs with dummy credentials, not Cloud responses.
        for line in result.stdout.splitlines():
            try:
                output = json.loads(line).get('Output')
            except json.JSONDecodeError:
                continue
            if output:
                print(output, end='')
        raise
    print('Exact hosted fixture passed:', ROOT + '/' + case)


if __name__ == '__main__':
    try:
        main()
    except (OSError, ValueError, subprocess.TimeoutExpired) as exc:
        raise SystemExit(str(exc)) from exc

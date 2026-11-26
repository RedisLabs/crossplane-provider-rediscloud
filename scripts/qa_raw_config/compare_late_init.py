"""Hosted-only experiment: compare the same fake fixture with one generated filter."""
import json
import os
from pathlib import Path
import sys

SOURCE = Path('apis/subscription/v1beta1/zz_database_terraformed.go')
ANCHOR = 'opts := []resource.GenericLateInitializerOption{resource.WithZeroValueJSONOmitEmptyFilter(resource.CNameWildcard)}'
FILTER = '\n\topts = append(opts, resource.WithNameFilter("SourceIps"))'


def main():
    if os.environ.get('GITHUB_ACTIONS') != 'true':
        raise SystemExit('hosted execution required')
    if sys.argv[1:] == ['prepare']:
        text = SOURCE.read_text()
        if text.count(ANCHOR) != 1 or FILTER in text:
            raise SystemExit('unexpected generated source')
        SOURCE.write_text(text.replace(ANCHOR, ANCHOR + FILTER, 1))
        print('Applied one disposable SourceIps late-initialization filter')
        return
    if len(sys.argv) != 7 or sys.argv[1] != 'verify' or sys.argv[2] not in {'lifecycle', 'source_cidrs', 'policy_transition'}:
        raise SystemExit('invalid comparison arguments')
    baseline, candidate, old_payloads, new_payloads = map(Path, sys.argv[3:])
    old_phases = baseline.read_text().splitlines()
    new_phases = candidate.read_text().splitlines()
    if old_phases.count('phase after_observe nonempty') != 1:
        raise SystemExit('baseline spec was not nonempty after observe')
    expected = 'nonempty' if sys.argv[2] == 'source_cidrs' else 'absent'
    for phase in ('before_create', 'after_create', 'after_observe', 'after_update'):
        if new_phases.count('phase ' + phase + ' ' + expected) != 1:
            raise SystemExit('candidate spec presence changed unexpectedly')
    old = [json.loads(line) for line in old_payloads.read_text().splitlines()]
    new = [json.loads(line) for line in new_payloads.read_text().splitlines()]
    if len(old) < 2 or len(new) < 2:
        raise SystemExit('fake lifecycle did not record both initial and later update')
    if old != new:
        print('baseline fake sourceIp payloads:', json.dumps(old, sort_keys=True))
        print('candidate fake sourceIp payloads:', json.dumps(new, sort_keys=True))
        raise SystemExit('fake sourceIp payload sequence changed')
    print('Fake sourceIp payload sequence matched across both fixture runs:', len(old))


if __name__ == '__main__':
    main()

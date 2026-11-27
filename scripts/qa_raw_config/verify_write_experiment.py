"""Validate fake PUTs for an unshipped provider and disposable Upjet filter."""
import json
from pathlib import Path
import sys

if len(sys.argv) != 5 or sys.argv[1] not in {'lifecycle', 'source_cidrs', 'policy_transition'} or sys.argv[2] not in {'baseline', 'filtered'}:
    raise SystemExit('invalid experiment case')
case, variant = sys.argv[1:3]
phases = Path(sys.argv[3]).read_text().splitlines()
rows = [json.loads(line) for line in Path(sys.argv[4]).read_text().splitlines()]
if len(rows) != 2 or any(set(row) != {'present', 'value'} for row in rows):
    raise SystemExit('expected exactly two fake database PUT records')
expected_phase = 'nonempty' if case == 'source_cidrs' or variant == 'baseline' else 'absent'
if phases.count('phase after_observe ' + expected_phase) != 1:
    raise SystemExit('unexpected source spec after observation')
private = {'10.0.0.0/8', '172.16.0.0/12', '192.168.0.0/16', '100.64.0.0/10'}
first = {'lifecycle': {'0.0.0.0/0'}, 'source_cidrs': {'192.0.2.0/24'}, 'policy_transition': private}[case]
if not rows[0]['present'] or not isinstance(rows[0]['value'], list) or set(rows[0]['value']) != first:
    raise SystemExit('unexpected post-create source CIDRs')
if case == 'source_cidrs':
    if not rows[1]['present'] or rows[1]['value'] != ['198.51.100.0/24']:
        raise SystemExit('explicit changed source CIDRs were not sent')
elif variant == 'filtered':
    if rows[1] != {'present': False, 'value': None}:
        raise SystemExit('expected sourceIp key to be absent on unrelated update')
elif not rows[1]['present'] or not isinstance(rows[1]['value'], list) or set(rows[1]['value']) != first:
    raise SystemExit('expected late-initialized source CIDRs to remain unchanged')
print('Fake candidate PUT assertions passed:', case, variant)

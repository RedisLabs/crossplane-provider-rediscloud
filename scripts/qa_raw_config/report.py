"""Validate and report category-only traces with bounded phase attribution."""
from pathlib import Path
import sys

if len(sys.argv) != 3 or sys.argv[1] not in {'lifecycle', 'source_cidrs', 'policy_transition'}:
    raise SystemExit('unexpected fixture case')
lines = Path(sys.argv[2]).read_text().splitlines()
valid = {'root-null', 'root-unknown', 'missing-attribute', 'unknown', 'null', 'known-nonnull'}
phases = ['before_create', 'after_create', 'after_observe', 'after_update']
phase_index = -1
observed = set()
changes = set()
awaiting_change = False
specs = []
for line in lines:
    fields = line.split(' ')
    if awaiting_change:
        if len(fields) != 2 or fields[0] != 'source-change' or fields[1] not in {'true', 'false'}:
            raise SystemExit('missing or invalid update change flag')
        changes.add((phases[phase_index], fields[1]))
        awaiting_change = False
        continue
    if len(fields) == 3 and fields[0] == 'phase':
        phase_index += 1
        if phase_index >= len(phases) or fields[1] != phases[phase_index] or fields[2] not in {'absent', 'empty', 'nonempty'}:
            raise SystemExit('invalid or out-of-order phase')
        specs.append((fields[1], fields[2]))
    elif len(fields) == 2 and fields[0] in {'create', 'read', 'update'} and fields[1] in valid and phase_index >= 0:
        observed.add((phases[phase_index], fields[0], fields[1]))
        awaiting_change = fields[0] == 'update'
    else:
        raise SystemExit('invalid fixture trace')
if phase_index != len(phases) - 1 or awaiting_change:
    raise SystemExit('fixture phases missing')
if not any(p == 'before_create' and op == 'create' for p, op, _ in observed):
    raise SystemExit('create callback not observed in create phase')
if not any(p == 'after_observe' and op == 'update' for p, op, _ in observed):
    raise SystemExit('update callback not observed in update phase')
if not any(op == 'read' for _, op, _ in observed):
    raise SystemExit('read callback not observed')
print('Fixture:', sys.argv[1])
for phase, presence in specs:
    print('spec', phase, presence)
for phase, operation, category in sorted(observed):
    print('callback', phase, operation, category)
for phase, changed in sorted(changes):
    print('source-change', phase, changed)

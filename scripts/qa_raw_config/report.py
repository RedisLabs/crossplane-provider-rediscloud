"""Reject malformed instrumentation and print only callback/category pairs."""
from pathlib import Path
import sys

if len(sys.argv) != 3 or sys.argv[1] not in {"lifecycle", "source_cidrs"}:
    raise SystemExit('unexpected fixture case')
lines = Path(sys.argv[2]).read_text().splitlines()
valid = {'root-null', 'root-unknown', 'missing-attribute', 'unknown', 'null', 'known-nonnull'}
seen = set()
for line in lines:
    fields = line.split(' ')
    if len(fields) != 2 or fields[0] not in {'create', 'read', 'update'} or fields[1] not in valid:
        raise SystemExit('invalid fixture trace')
    seen.add(tuple(fields))
if not {'create', 'read', 'update'}.issubset({operation for operation, _ in seen}):
    raise SystemExit('expected callback observations missing')
print("Fixture:", sys.argv[1])
for operation, category in sorted(seen):
    print(operation, category)

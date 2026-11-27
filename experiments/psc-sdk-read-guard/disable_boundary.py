"""Disable the Crossplane containment only in this disposable CI checkout."""
from pathlib import Path

path = Path("internal/controller/lookup/sources_connectivity.go")
text = path.read_text()
old = '''func safePSCRead[T any](read func() (*T, error)) (result *T, err error) {
\tcompleted := false
\tdefer func() {
\t\tif !completed {
\t\t\t_ = recover()
\t\t\tresult = nil
\t\t\terr = fmt.Errorf("private service connect lookup failed")
\t\t}
\t}()
\tresult, err = read()
\tcompleted = true
\treturn result, err
}'''
new = '''func safePSCRead[T any](read func() (*T, error)) (*T, error) {
\treturn read()
}'''
if text.count(old) != 1:
    raise SystemExit("unexpected PSC containment source")
path.write_text(text.replace(old, new, 1))
print("Disabled PSC panic containment in disposable runner checkout")

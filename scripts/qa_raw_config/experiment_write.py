"""Apply an intentionally unshipped candidate to a disposable pinned checkout."""
from pathlib import Path
import sys

root = Path(sys.argv[1])
source = root / 'provider/pro/resource_rediscloud_pro_database.go'
text = source.read_text()
changes = [
    ('resourceRedisCloudProDatabaseUpdateWithTimeout(ctx, d, meta, timeout)',
     'resourceRedisCloudProDatabaseUpdateWithTimeout(ctx, d, meta, timeout, true)'),
    ('resourceRedisCloudProDatabaseUpdateWithTimeout(ctx, d, meta, d.Timeout(schema.TimeoutUpdate))',
     'resourceRedisCloudProDatabaseUpdateWithTimeout(ctx, d, meta, d.Timeout(schema.TimeoutUpdate), false)'),
    ('func resourceRedisCloudProDatabaseUpdateWithTimeout(ctx context.Context, d *schema.ResourceData, meta interface{}, timeout time.Duration) diag.Diagnostics {',
     'func resourceRedisCloudProDatabaseUpdateWithTimeout(ctx context.Context, d *schema.ResourceData, meta interface{}, timeout time.Duration, postCreate bool) diag.Diagnostics {'),
    ('\tif isDefaultSourceIPs(sourceIPs) {', '\tif postCreate && isDefaultSourceIPs(sourceIPs) {'),
]
for old, new in changes:
    if text.count(old) != 1:
        raise SystemExit('candidate patch anchor mismatch')
    text = text.replace(old, new, 1)
anchor = '\n\t// One of the following fields must be set, validation is handled in the schema (ExactlyOneOf)'
if text.count(anchor) != 1:
    raise SystemExit('candidate raw-config anchor mismatch')
guard = '''
\tif !postCreate {
\t\traw := d.GetRawConfig()
\t\tif !raw.IsKnown() || raw.IsNull() || !raw.Type().IsObjectType() || !raw.Type().HasAttribute("source_ips") {
\t\t\tutils.SubscriptionMutex.Unlock(subId)
\t\t\treturn diag.Errorf("source CIDR configuration is unavailable")
\t\t}
\t\tconfigured := raw.GetAttr("source_ips")
\t\tif !configured.IsKnown() {
\t\t\tutils.SubscriptionMutex.Unlock(subId)
\t\t\treturn diag.Errorf("source CIDR configuration is unknown")
\t\t}
\t\tif configured.IsNull() {
\t\t\tupdate.SourceIP = nil
\t\t}
\t}
'''
text = text.replace(anchor, guard + anchor, 1)
source.write_text(text)
print('Applied experimental source CIDR write logic to disposable provider checkout')

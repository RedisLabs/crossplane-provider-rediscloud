"""Instrument an isolated candidate checkout for hosted fake-only observation."""
from pathlib import Path
import sys

root = Path(sys.argv[1])
source = root / 'provider/pro/resource_rediscloud_pro_database.go'
text = source.read_text()
for operation, signature in (
    ('create', 'func resourceRedisCloudProDatabaseCreate(ctx context.Context, d *schema.ResourceData, meta interface{}) diag.Diagnostics {'),
    ('read', 'func resourceRedisCloudProDatabaseReadWithTimeout(ctx context.Context, d *schema.ResourceData, meta interface{}, timeout time.Duration) diag.Diagnostics {'),
    ('update', 'func resourceRedisCloudProDatabaseUpdateWithTimeout(ctx context.Context, d *schema.ResourceData, meta interface{}, timeout time.Duration) diag.Diagnostics {'),
):
    if text.count(signature) != 1:
        raise SystemExit('expected exactly one instrumentation anchor')
    text = text.replace(signature, signature + '\n\tqaRecordRawConfig(d, "' + operation + '")', 1)
source.write_text(text)
(root / 'provider/pro/qa_raw_config_fixture.go').write_text('''package pro

import (
 "fmt"
 "os"
 "github.com/hashicorp/terraform-plugin-sdk/v2/helper/schema"
)

// Test-only instrumentation: record categories, never configuration values.
func qaRecordRawConfig(d *schema.ResourceData, operation string) {
 path := os.Getenv("QA_RAW_CONFIG_TRACE")
 if path == "" { panic("missing fixture trace path") }
 category := "root-null"
 raw := d.GetRawConfig()
 if !raw.IsKnown() { category = "root-unknown" } else if !raw.IsNull() {
  if !raw.Type().IsObjectType() || !raw.Type().HasAttribute("source_ips") { category = "missing-attribute" } else {
   value := raw.GetAttr("source_ips")
   switch { case !value.IsKnown(): category = "unknown"; case value.IsNull(): category = "null"; default: category = "known-nonnull" }
  }
 }
 f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
 if err != nil { panic("cannot open fixture trace") }
 if _, err = fmt.Fprintf(f, "%s %s\\n", operation, category); err != nil { panic("cannot write fixture trace") }
 if err = f.Close(); err != nil { panic("cannot close fixture trace") }
}
''')

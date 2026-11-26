#!/usr/bin/env python3
"""Correct known Upjet generation defects:
1. Fix broken ActiveSubscriptionDatabaseInitParameters.ClientTLSCertificates SecretKeySelector type.
2. Adjust controller reconciler timeout from 3m to 30m for long-running Redis Cloud operations.
3. Make ACL user password selectors structurally optional so the existing CEL
   requirement can distinguish Observe-only from Create/Update policies.
"""

import pathlib
import sys

ROOT = pathlib.Path(__file__).resolve().parents[1]
TARGET = ROOT / "apis/active/v1beta1/zz_activesubscriptiondatabase_types.go"
OLD_TLS = 'ClientTLSCertificates []*string `json:"clientTlsCertificatesSecretRef,omitempty" tf:"-"`'
NEW_TLS = 'ClientTLSCertificatesSecretRef *[]v2.SecretKeySelector `json:"clientTlsCertificatesSecretRef,omitempty" tf:"-"`'
ACL_PASSWORD_OLD = 'PasswordSecretRef v2.SecretKeySelector `json:"passwordSecretRef" tf:"-"`'
ACL_PASSWORD_NEW = 'PasswordSecretRef *v2.SecretKeySelector `json:"passwordSecretRef,omitempty" tf:"-"`'


def fix_acl_user_password() -> int:
    for version in ("v1alpha1", "v1beta1"):
        path = ROOT / "apis" / "acl" / version / "zz_user_types.go"
        if not path.exists():
            print(f"missing generated file: {path}", file=sys.stderr)
            return 1
        text = path.read_text()
        if text.count(ACL_PASSWORD_OLD) == 2 and ACL_PASSWORD_NEW not in text:
            path.write_text(text.replace(ACL_PASSWORD_OLD, ACL_PASSWORD_NEW))
            print(f"corrected {path.relative_to(ROOT)}")
        elif text.count(ACL_PASSWORD_NEW) != 2 or ACL_PASSWORD_OLD in text:
            print(f"unexpected ACL user password fields in {path}", file=sys.stderr)
            return 1
    return 0


def fix_tls_type() -> int:
    if not TARGET.exists():
        print(f"missing generated file: {TARGET}", file=sys.stderr)
        return 1
    text = TARGET.read_text()
    if OLD_TLS in text:
        TARGET.write_text(text.replace(OLD_TLS, NEW_TLS, 1))
        print(f"corrected {TARGET.relative_to(ROOT)}")
    elif NEW_TLS in text:
        print("generated TLS initProvider field is already corrected")
    else:
        print(f"expected TLS field not found in {TARGET}", file=sys.stderr)
        return 1
    return 0


def fix_controller_timeouts() -> int:
    controllers_dir = ROOT / "internal/controller"
    if not controllers_dir.exists():
        return 0
    count = 0
    for path in controllers_dir.glob("*/*/zz_controller.go"):
        text = path.read_text()
        updated = text
        for old in ("managed.WithTimeout(3 * time.Minute)", "managed.WithTimeout(3*time.Minute)"):
            if old in updated:
                updated = updated.replace(old, "managed.WithTimeout(30 * time.Minute)")
        if updated != text:
            path.write_text(updated)
            count += 1
    if count > 0:
        print(f"updated timeout to 30m in {count} controller(s)")
    return 0


def main() -> int:
    if fix_tls_type() != 0:
        return 1
    if fix_acl_user_password() != 0:
        return 1
    if fix_controller_timeouts() != 0:
        return 1
    return 0


if __name__ == "__main__":
    raise SystemExit(main())

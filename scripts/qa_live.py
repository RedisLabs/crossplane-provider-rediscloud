#!/usr/bin/env python3
"""Create and delete QA objects that need only the Redis Cloud API.

The suite creates one ACL rule, role, and user, one Pro subscription and
database, and one Essentials subscription and database. It deletes each
before the run finishes.

The workflow that calls this script triggers on schedule and workflow_dispatch
only. It does not run on pull requests. Logs print status codes, Ready
reasons, and secret key names. They do not print secret values, API keys, or
the payment method id.
"""

from __future__ import annotations

import argparse
import base64
import hashlib
import hmac
import http.client
import json
import os
import re
import secrets
import string
import subprocess
import sys
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
from datetime import datetime, timedelta, timezone
from pathlib import Path

PREFIX = "xp-ci-"
REQUIRED_SECRET_KEYS = ("public_endpoint", "private_endpoint")
ALLOWED_CONTEXT = "kind-local-dev"
ACL_RULE = "+@read ~*"
DEFAULT_API = "https://api.redislabs.com/v1"
MAX_HTTP_BODY_BYTES = 8 * 1024 * 1024
HELD_CIDRS = {
    "10.13.90.0/24",
    "10.13.91.0/24",
    "10.13.92.0/24",
    "10.13.99.0/24",
    "10.0.0.0/24",
    "10.27.30.0/24",
}

SUBSCRIPTION = "subscription.rediscloud.redis.io"
DATABASE = "database.subscription.redis.io"
ESS_SUBSCRIPTION = "subscription.essentials.redis.io"
ESS_DATABASE = "database.essentials.redis.io"
ACL = "rule.acl.redis.io"
ROLE = "role.acl.redis.io"
USER = "user.acl.redis.io"
PAYMENT = "paymentmethod.lookup.redis.io"
PLAN = "essentialsplan.lookup.redis.io"
REGIONS = "regions.lookup.redis.io"
PROVIDER_CONFIG = "providerconfig.rediscloud.redis.io"


class SuiteFailure(Exception):
    """The QA suite cannot continue. The message must not contain secrets."""


def assert_safe_context(context: str) -> None:
    if context != ALLOWED_CONTEXT:
        raise SuiteFailure("refusing to run outside kind-local-dev")


def normalize_credentials(raw: dict) -> dict:
    api_key = str(raw.get("api_key") or "").strip()
    secret_key = str(raw.get("secret_key") or raw.get("api_secret_key") or "").strip()
    url = str(raw.get("url") or raw.get("base_url") or "").strip()
    if not api_key or not secret_key:
        raise SuiteFailure("credentials require api_key and secret_key")
    out = {"api_key": api_key, "secret_key": secret_key}
    if url:
        out["url"] = url.rstrip("/")
    return out


def matches_expected_qa_origin(url: str) -> bool:
    actual = urllib.parse.urlsplit(url)
    origin_hash = hashlib.sha256(f"{actual.scheme}://{actual.netloc}".encode()).hexdigest()
    allowed = [value.strip() for value in os.environ.get("QA_VERIFIED_ORIGIN_HASHES", "").split(",")]
    if not allowed or not all(re.fullmatch(r"[0-9a-f]{64}", value) for value in allowed):
        return False
    return bool(
        actual.scheme == "https" and actual.netloc
        and origin_hash in allowed
        and not actual.username and not actual.password
        and actual.path.rstrip("/") == "/v1"
        and not (actual.query or actual.fragment)
    )


def verify_checkpoint_artifact(run_id: str, credentials: dict, expected_kind: str = "strict") -> None:
    """Authenticate the artifact downloaded from this exact hosted run."""
    run = os.environ.get("GITHUB_RUN_ID", "")
    attempt = os.environ.get("GITHUB_RUN_ATTEMPT", "")
    artifact_id = os.environ.get("QA_OWNERSHIP_ARTIFACT_ID", "")
    artifact_path = os.environ.get("QA_OWNERSHIP_ARTIFACT_PATH", "")
    commit = os.environ.get("GITHUB_SHA", "")
    if (os.environ.get("GITHUB_ACTIONS") != "true" or
            os.environ.get("GITHUB_REPOSITORY") != "RedisLabs/crossplane-provider-rediscloud" or
            not re.fullmatch(r"[1-9][0-9]*", run) or not re.fullmatch(r"[1-9][0-9]*", attempt) or
            not re.fullmatch(r"[1-9][0-9]*", artifact_id) or not artifact_path or not re.fullmatch(r"[a-f0-9]{40}", commit) or
            run_id != f"{run}-{attempt}"):
        raise SuiteFailure("strict qualification requires this run's persisted ownership artifact")
    try:
        with tempfile.TemporaryDirectory(prefix="qa-ownership-") as tmp:
            encrypted = Path(tmp) / "ownership.enc"
            encrypted.write_bytes(Path(artifact_path).read_bytes())
            script = Path(__file__).with_name("qa_checkpoint.mjs")
            subprocess.run(["node", str(script), "resolve", str(encrypted), run, attempt],
                           check=True, capture_output=True, text=True)
            intent = json.loads(Path(str(encrypted) + ".resolved").read_text())
        key = base64.b64decode(os.environ.get("QA_CHECKPOINT_KEY", ""), validate=True)
        parts = urllib.parse.urlsplit(credentials.get("url") or "")
        origin_hash = hashlib.sha256(f"{parts.scheme}://{parts.netloc}".encode()).hexdigest()
        binding = hmac.new(key, credentials["api_key"].encode(), hashlib.sha256).hexdigest()
    except (OSError, ValueError, KeyError, subprocess.CalledProcessError):
        raise SuiteFailure("ownership artifact could not be authenticated") from None
    if (len(key) != 32 or intent.get("kind") != expected_kind or intent.get("commit") != commit or
            intent.get("originHash") != origin_hash or
            not hmac.compare_digest(binding, str(intent.get("credentialBinding") or ""))):
        raise SuiteFailure("ownership artifact does not match this run, commit and account")


def preflight(credentials: dict) -> None:
    """Authenticate read-only, without a Kubernetes context or any Cloud write."""
    if not credentials.get("url") or not matches_expected_qa_origin(credentials["url"]):
        raise SuiteFailure("preflight requires an independently verified QA endpoint")
    cloud = RedisCloud(credentials["url"], credentials["api_key"], credentials["secret_key"])
    code, body = cloud.request("GET", "/fixed/plans?provider=GCP")
    if code != 200 or not isinstance(body, dict) or not isinstance(body.get("plans"), list):
        raise SuiteFailure("GCP plans read failed or returned unexpected data")
    matches = [p for p in body["plans"] if isinstance(p, dict)
               and str(p.get("provider") or "").upper() == "GCP"
               and p.get("region") == "us-central1" and p.get("name") == "1GB"
               and p.get("size") == 1 and p.get("sizeMeasurementUnit") == "GB"
               and not p.get("supportReplication") and not p.get("supportDataPersistence")
               and p.get("id") is not None]
    if len(matches) != 1:
        raise SuiteFailure("the GCP Essentials plan lookup does not have exactly one match")
    print("QA endpoint verified; authenticated GCP plan lookup has one matching plan", flush=True)
    subscriptions = cloud.list_fixed_subscriptions_checked()
    print(f"authenticated Essentials subscription inventory count={len(subscriptions)}", flush=True)
    try:
        code, task_body = cloud.request("GET", "/tasks")
    except SuiteFailure:
        code, task_body = 0, None
    if code != 200 or not isinstance(task_body, (dict, list)):
        print("account task list unavailable; task coverage unverified", flush=True)
    else:
        root = "array" if isinstance(task_body, list) else next(
            (key for key in ("tasks", "task", "items", "results", "data")
             if isinstance(task_body.get(key), list)), "unrecognized")
        rows = task_body if isinstance(task_body, list) else task_body.get(root)
        if not isinstance(rows, list):
            print("account task response shape is not a recognized array; task coverage unverified", flush=True)
            rows = []
        schema = [key for key in ("taskId", "commandType", "status", "timestamp", "response")
                  if rows and all(isinstance(row, dict) and key in row for row in rows)]
        metadata = [key for key in ("total", "totalCount", "limit", "offset", "next")
                    if isinstance(task_body, dict) and key in task_body]
        print(f"authenticated account task list shape: root={root} count={len(rows)} common_fields={','.join(schema)} pagination_fields={','.join(metadata)}", flush=True)
        resource_count = sum(1 for row in rows if isinstance(row, dict) and isinstance(row.get("response"), dict)
                             and row["response"].get("resourceId") is not None)
        print(f"account task entries with response resource identity={resource_count}; task ownership not established", flush=True)
    if subscriptions:
        sub_id = subscriptions[0].get("id")
        if sub_id is None:
            raise SuiteFailure("subscription inventory has no identity for pagination check")
        cloud.list_fixed_databases_complete(str(sub_id))
        print("authenticated database pagination reached an empty page", flush=True)
    else:
        print("database pagination not exercised: account has no Essentials subscription", flush=True)


def task_diagnostic(credentials: dict, clock=None, output=print) -> None:
    """Read recent fixed-database delete task states without printing IDs."""
    if not credentials.get("url") or not matches_expected_qa_origin(credentials["url"]):
        raise SuiteFailure("task diagnostic requires a verified QA endpoint")
    import qa_capture_task_baseline
    cloud = RedisCloud(credentials["url"], credentials["api_key"], credentials["secret_key"])
    try:
        rows = qa_capture_task_baseline.task_rows(cloud)
    except qa_capture_task_baseline.qa_live.SuiteFailure:
        raise SuiteFailure("task diagnostic could not read tasks") from None
    now = clock or datetime.now(timezone.utc)
    cutoff = now - timedelta(hours=2)
    valid_status = {"initialized", "received", "processing-in-progress", "processing-completed", "processing-error"}
    found = []
    invalid_timestamps = 0
    for row in rows.values():
        if row.get("commandType") != "fixedDatabaseDeleteRequest":
            continue
        try:
            timestamp = datetime.fromisoformat(str(row.get("timestamp") or "").replace("Z", "+00:00"))
            if timestamp.tzinfo is None:
                raise ValueError("timezone required")
            timestamp = timestamp.astimezone(timezone.utc)
        except ValueError:
            invalid_timestamps += 1
            continue
        if cutoff <= timestamp <= now:
            status = row.get("status") if row.get("status") in valid_status else "other"
            found.append((timestamp, status))
    output(f"recent fixedDatabaseDeleteRequest task count={len(found)}; invalid timestamps={invalid_timestamps}")
    for timestamp, status in sorted(found, reverse=True)[:50]:
        output(f"fixedDatabaseDeleteRequest status={status} timestamp={timestamp.isoformat().replace('+00:00', 'Z')}")
    if len(found) > 50:
        output("additional matching tasks omitted from display")
    encoded_key = os.environ.get("QA_ACCOUNT_FINGERPRINT_KEY", "")
    try:
        key = base64.b64decode(encoded_key, validate=True)
    except ValueError:
        key = b""
    if len(key) == 32:
        code, body = cloud.request("GET", "/subscriptions")
        account_id = body.get("accountId") if code == 200 and isinstance(body, dict) else None
        if isinstance(account_id, bool) or not isinstance(account_id, (int, str)) or not str(account_id).isdigit() or int(account_id) < 1:
            output("account comparison unavailable: authenticated account ID not returned")
        else:
            normalized = str(int(account_id))
            fingerprint = hmac.new(key, normalized.encode(), hashlib.sha256).hexdigest()
            output(f"opaque account-ID comparison HMAC={fingerprint}")
    else:
        output("account comparison unavailable: shared fingerprint key missing or invalid")


def redact(text: str, secrets: list[str]) -> str:
    redacted = text
    for secret in secrets:
        if secret:
            redacted = redacted.replace(secret, "[redacted]")
    return redacted


def names_with_prefix(subscriptions: list, prefix: str = PREFIX) -> list:
    return [row for row in subscriptions if str(row.get("name") or "").startswith(prefix)]


def success_problems(
    subscription_status: str,
    database_status: str,
    secret_keys: list[str],
    database_delete_code: int,
    subscription_delete_code: int,
) -> list[str]:
    problems = []
    if subscription_status != "active":
        problems.append(f"subscription cloud status {subscription_status}")
    if database_status != "active":
        problems.append(f"database cloud status {database_status}")
    present = set(secret_keys)
    for key in REQUIRED_SECRET_KEYS:
        if key not in present:
            problems.append(f"missing secret key {key}")
    if database_delete_code != 404:
        problems.append(f"database delete HTTP {database_delete_code}")
    if subscription_delete_code != 404:
        problems.append(f"subscription delete HTTP {subscription_delete_code}")
    return problems


def choose_cidr(run_id: str, used: set[str]) -> str:
    digits = "".join(ch for ch in run_id if ch.isdigit()) or "1"
    seed = int(digits)
    for step in range(4096):
        second = 20 + ((seed + step) % 200)
        third = (seed * 3 + step * 17) % 256
        cidr = f"10.{second}.{third}.0/24"
        if cidr in HELD_CIDRS or cidr in used:
            continue
        return cidr
    raise SuiteFailure("no free CIDR")


def used_cidrs(subscriptions: list) -> set[str]:
    found = set()
    for sub in subscriptions:
        for detail in sub.get("cloudDetails") or []:
            for region in detail.get("regions") or []:
                for net in region.get("networking") or []:
                    cidr = net.get("deploymentCIDR") or net.get("deploymentCidr")
                    if cidr:
                        found.add(str(cidr))
    return found


def secret_key_names(secret: dict) -> list[str]:
    data = secret.get("data") or {}
    if not isinstance(data, dict):
        raise SuiteFailure("secret data is not an object")
    return sorted(str(key) for key in data)


def subscription_rows(body) -> list:
    if isinstance(body, dict):
        return list(body.get("subscriptions") or [])
    if isinstance(body, list):
        return body
    return []


def validated_account_rows(body, key: str) -> list:
    # The SDK lists these account resources in one response, with no paging
    # controls. Refuse any evidence of a partial or unfamiliar response.
    paging = {"offset", "limit", "page", "pageSize", "total", "totalCount",
              "totalRecords", "next", "nextPage", "hasMore", "pagination", "links"}
    if not isinstance(body, dict) or paging.intersection(body) or not isinstance(body.get(key), list):
        raise SuiteFailure("account inventory has unexpected shape or pagination")
    rows = body[key]
    if len(rows) >= 10000:
        raise SuiteFailure("account inventory exceeds safe bound")
    ids, names = set(), set()
    for row in rows:
        if not isinstance(row, dict):
            raise SuiteFailure("account inventory contains malformed row")
        identifier, name = row.get("id"), row.get("name")
        if (isinstance(identifier, bool) or not isinstance(identifier, (int, str)) or
                not str(identifier).isdigit() or int(identifier) <= 0 or
                not isinstance(name, str) or not name.strip()):
            raise SuiteFailure("account inventory contains malformed identity")
        normalized = str(int(identifier))
        if normalized in ids or name in names:
            raise SuiteFailure("account inventory contains duplicate identity")
        ids.add(normalized)
        names.add(name)
    return rows



def validated_database_page(body, parent, rows, seen, offset, limit, totals, metadata, label):
    """Reject contradictory metadata and malformed or duplicated rows."""
    page = parent["databases"]
    if metadata.get("hasMore") is True and not page:
        raise SuiteFailure(f"{label} preceding page reported more data")
    if metadata.get("hasMore") is False and page:
        raise SuiteFailure(f"{label} preceding page reported no more data")
    flags = set()
    if len(page) > limit:
        raise SuiteFailure(f"{label} page exceeds limit")
    for obj in (body, parent):
        for key in ("total", "totalCount", "totalRecords", "numberOfDatabases"):
            if key in obj:
                count = obj[key]
                if type(count) is not int or count < 0:
                    raise SuiteFailure(f"{label} count malformed")
                totals.add(count)
        for key in ("next", "nextPage", "nextPageToken", "cursor"):
            if obj.get(key):
                raise SuiteFailure(f"{label} pagination metadata unsupported")
        if "hasMore" in obj:
            if type(obj["hasMore"]) is not bool:
                raise SuiteFailure(f"{label} hasMore malformed")
            flags.add(obj["hasMore"])
        if obj.get("hasMore") is True and not page:
            raise SuiteFailure(f"{label} empty page reports more data")
        for key in ("links", "_links", "pagination"):
            if obj.get(key):
                raise SuiteFailure(f"{label} pagination metadata unsupported")
        for key, expected in (("offset", offset), ("limit", limit)):
            if key in obj and (type(obj[key]) is not int or obj[key] != expected):
                raise SuiteFailure(f"{label} pagination metadata differs")
    if len(flags) > 1:
        raise SuiteFailure(f"{label} pagination flags conflict")
    metadata["hasMore"] = next(iter(flags)) if flags else None
    if len(totals) > 1:
        raise SuiteFailure(f"{label} counts conflict")
    for row in page:
        if not isinstance(row, dict):
            raise SuiteFailure(f"{label} row malformed")
        ident = row.get("databaseId")
        if (isinstance(ident, bool) or not isinstance(ident, (int, str)) or
                not re.fullmatch(r"[0-9]{1,20}", str(ident)) or int(ident) <= 0 or
                not isinstance(row.get("name"), str) or not row["name"].strip()):
            raise SuiteFailure(f"{label} identity malformed")
        ident = str(int(ident))
        if ident in seen:
            raise SuiteFailure(f"{label} duplicate identity")
        seen.add(ident)
        rows.append(row)
    if totals and (len(rows) > next(iter(totals)) or
                   (not page and len(rows) != next(iter(totals)))):
        raise SuiteFailure(f"{label} inventory count mismatch")
    return bool(page)

def database_rows(body) -> list:
    if not isinstance(body, dict):
        return []
    rows = []
    for sub in body.get("subscription") or []:
        if isinstance(sub, dict):
            rows.extend(sub.get("databases") or [])
    return rows


def acl_rows(body) -> list:
    return named_rows(body, "redisRules")


def named_rows(body, key: str) -> list:
    if isinstance(body, dict):
        return list(body.get(key) or [])
    if isinstance(body, list):
        return body
    return []


def fixed_database_rows(body) -> list:
    if not isinstance(body, dict):
        return []
    sub = body.get("subscription")
    if isinstance(sub, dict):
        return list(sub.get("databases") or [])
    rows = []
    for item in sub or []:
        if isinstance(item, dict):
            rows.extend(item.get("databases") or [])
    return rows


def is_ready(obj: dict | None) -> bool:
    if not obj:
        return False
    for condition in obj.get("status", {}).get("conditions") or []:
        if condition.get("type") == "Ready" and condition.get("status") == "True":
            return True
    return False


def strict_ready(obj: dict | None) -> bool:
    conditions = (obj or {}).get("status", {}).get("conditions") or []
    return all(any(c.get("type") == kind and c.get("status") == "True" for c in conditions)
               for kind in ("Ready", "Synced"))


def strict_identity(obj: dict | None, label: str) -> str:
    metadata = (obj or {}).get("metadata") or {}
    if not metadata.get("uid") or "finalizer.managedresource.crossplane.io" not in (metadata.get("finalizers") or []):
        raise SuiteFailure(f"{label} has no UID or Crossplane managed-resource finalizer")
    if not strict_ready(obj):
        raise SuiteFailure(f"{label} is not Ready and Synced")
    return str(((obj or {}).get("status") or {}).get("atProvider", {}).get("id") or "")


def ready_reason(obj: dict | None) -> str:
    if not obj:
        return "missing"
    for condition in obj.get("status", {}).get("conditions") or []:
        if condition.get("type") == "Ready":
            return str(condition.get("reason") or "")
    return ""


def ready_message(obj: dict | None) -> str:
    if not obj:
        return ""
    for condition in obj.get("status", {}).get("conditions") or []:
        if condition.get("type") == "Ready":
            return str(condition.get("message") or "")
    return ""


def numeric_id(value: str):
    text = str(value).strip()
    number = float(text)
    if number.is_integer():
        return int(number)
    return number


def database_number(value) -> str:
    """Return the database segment of a Terraform id.

    Pro and Essentials databases store status.atProvider.id as
    subscription/database. A plain database number is unchanged.
    """
    text = str(value).strip()
    parts = text.split("/")
    if len(parts) == 2:
        text = parts[1].strip()
    elif len(parts) != 1:
        raise SuiteFailure("database id is not a number")
    if not text.isdigit():
        raise SuiteFailure("database id is not a number")
    return text


def observed_database_id(row: dict | None, cr_id) -> str:
    cloud_id = None
    if isinstance(row, dict):
        cloud_id = row.get("databaseId")
        if cloud_id in (None, ""):
            cloud_id = row.get("id")
    raw = cloud_id if cloud_id not in (None, "") else cr_id
    return database_number(raw)


class State:
    def __init__(self, run_name: str):
        self.run_name = run_name
        self.subscription_name = None
        self.database_name = None
        self.acl_name = None
        self.role_name = None
        self.user_name = None
        self.password_secret = None
        self.subscription_id = None
        self.database_id = None
        self.essentials_name = None
        self.essentials_id = None
        self.essentials_database_name = None
        self.essentials_database_id = None
        self.subscription_deleted = False
        self.database_deleted = False
        self.essentials_deleted = False
        self.essentials_database_deleted = False
        self.lookups_applied = False
        self.named_deletion_requested = set()
        self.deletion_requested = set()
        self.owned_uids = {}


def create_owned(deps, state, resource, manifest, namespace=None):
    """Create (never apply over) one object and retain its server-assigned UID."""
    name = manifest["metadata"]["name"]
    uid = deps.cluster.create_owned(manifest)
    if not isinstance(uid, str) or not uid:
        raise SuiteFailure("created resource UID unavailable; ownership uncertain")
    state.owned_uids[(resource, name, namespace)] = uid


def delete_owned(deps, state, resource, name, namespace=None):
    """Delete only the exact object created by this run, atomically at the API."""
    uid = state.owned_uids.get((resource, name, namespace))
    if not uid:
        raise SuiteFailure("resource UID unavailable; refusing deletion")
    deps.cluster.delete_owned(resource, name, uid, namespace)
    state.deletion_requested.add((resource, name, namespace))


def run(deps) -> None:
    """Run the live order. Raise SuiteFailure without creating a second xp-ci- subscription."""
    if not clear_or_refuse(deps):
        raise SuiteFailure("xp-ci- subscription still present")
    deps.cluster.install_provider_config()
    apply_lookups(deps)
    payment_id = str(deps.cluster.payment_method_id() or "").strip()
    if not payment_id:
        raise SuiteFailure("payment method lookup has no id")
    deps.remember_secret(payment_id)
    state = State(f"{PREFIX}{deps.run_id}")
    cleanup_complete = True
    try:
        create_acl(deps, state)
        run_subscription(deps, state, payment_id)
        run_database(deps, state)
        run_acl_role_and_user(deps, state)
        delete_acl(deps, state)
        keys = list(deps.cluster.secret_key_names("default", state.run_name))
        deps.log("secret keys " + ",".join(keys))
        missing_keys = [key for key in REQUIRED_SECRET_KEYS if key not in keys]
        if missing_keys:
            raise SuiteFailure("missing secret key " + ",".join(missing_keys))
        database_code = delete_database(deps, state)
        if database_code != 404:
            raise SuiteFailure("database absence unverified; preserving parent")
        subscription_code = delete_subscription(deps, state)
        problems = success_problems("active", "active", keys, database_code, subscription_code)
        if problems:
            raise SuiteFailure("; ".join(problems))
        run_essentials(deps, state, payment_id)
        deps.log("qa live ok")
    except Exception:
        cleanup_complete = False
        deadline = deps.now() + deps.timeouts["recovery_hold"]
        deps.recovery_deadline = deadline
        while True:
            try:
                cleanup_complete = cleanup(deps, state)
            except Exception:
                cleanup_complete = False
                deps.log("standard recovery uncertain; preserving controller and credentials")
            if cleanup_complete or deps.now() >= deadline:
                break
            deps.log("standard recovery hold: unresolved; retaining controller and credentials")
            deps.sleep(min(deps.recovery_poll, max(0, deadline - deps.now())))
        deps.recovery_deadline = None
        if not cleanup_complete:
            deps.log("standard recovery hold timed out; residue is unverified")
        raise
    finally:
        if cleanup_complete:
            delete_lookups(deps)


def cleanup_only(deps) -> None:
    """Report shared-prefix leftovers without deleting unowned resources."""
    if not clear_or_refuse(deps):
        raise SuiteFailure("xp-ci- resources still present; ownership unverified")
    deps.log("authenticated shared-prefix inventory empty")


def clear_or_refuse(deps) -> bool:
    if prefixed_leftovers(deps):
        deps.log("pre-existing xp-ci- resources; refusing mutation without ownership")
        return False
    return True


def prefixed_leftovers(deps) -> bool:
    return any(
        names_with_prefix(rows)
        for rows in (
            deps.cloud.list_subscriptions(),
            deps.cloud.list_fixed_subscriptions(),
            deps.cloud.list_acl_roles(),
            deps.cloud.list_acl_users(),
            deps.cloud.list_acl_rules(),
        )
    )


def create_acl(deps, state: State) -> None:
    state.acl_name = state.run_name
    create_owned(deps, state, ACL, acl_manifest(state.run_name))
    if not wait_until(
        deps,
        f"ACL {state.run_name}",
        deps.timeouts["acl"],
        lambda: acl_is_active(deps, state.run_name),
    ):
        raise SuiteFailure(f"ACL {state.run_name} did not become active")


def delete_acl(deps, state: State) -> None:
    delete_owned(deps, state, ACL, state.run_name)
    if not wait_until(
        deps,
        f"ACL {state.run_name} delete",
        deps.timeouts["delete"],
        lambda: acl_is_gone(deps, state.run_name),
    ):
        raise SuiteFailure(f"ACL {state.run_name} is still present")
    state.acl_name = None
    deps.log(f"ACL {state.run_name} created and gone")


def acl_is_active(deps, name: str) -> bool:
    cr = deps.cluster.get(ACL, name)
    rule = find_named(deps.cloud.list_acl_rules(), name)
    status = str((rule or {}).get("status") or "missing")
    text = str((rule or {}).get("acl") or "")
    deps.log(f"ACL {name} ready={is_ready(cr)} cloud={status}")
    return is_ready(cr) and status == "active" and text == ACL_RULE


def acl_is_gone(deps, name: str) -> bool:
    gone = find_named(deps.cloud.list_acl_rules(), name) is None
    deps.log(f"ACL {name} gone={gone}")
    return gone


def run_subscription(deps, state: State, payment_id: str) -> None:
    state.subscription_name = state.run_name
    cidr = choose_cidr(deps.run_id, used_cidrs(deps.cloud.list_subscriptions()))
    deps.log(f"subscription {state.run_name} cidr {cidr}")
    create_owned(deps, state, SUBSCRIPTION, subscription_manifest(state.run_name, payment_id, cidr))
    holder = {}
    if not wait_until(
        deps,
        f"subscription {state.run_name}",
        deps.timeouts["subscription"],
        lambda: subscription_is_active(deps, state.run_name, holder),
    ):
        raise SuiteFailure(f"subscription {state.run_name} did not become active")
    state.subscription_id = str(holder["id"])
    deps.remember_secret(state.subscription_id)
    deps.log(f"subscription {state.run_name} cloud=active ready=true")


def subscription_is_active(deps, name: str, holder: dict) -> bool:
    row = find_named(deps.cloud.list_subscriptions(), name)
    status = str((row or {}).get("status") or "missing")
    cr = deps.cluster.get(SUBSCRIPTION, name)
    deps.log(f"subscription {name} cloud={status} ready={is_ready(cr)} reason={ready_reason(cr)}")
    if status == "error":
        raise SuiteFailure(f"subscription {name} cloud status error")
    if row is None and "400" in ready_message(cr):
        raise SuiteFailure(f"subscription {name} was rejected by the API")
    cloud_id = None if row is None else row.get("id")
    cr_id = ((cr or {}).get("status") or {}).get("atProvider", {}).get("id")
    if status == "active" and is_ready(cr) and cr_id not in (None, ""):
        holder["id"] = cr_id
        if cloud_id is not None:
            holder["id"] = cr_id
        return True
    return False


def run_database(deps, state: State) -> None:
    state.database_name = state.run_name
    create_owned(deps, state, DATABASE, database_manifest(state.run_name, state.subscription_id))
    holder = {}
    if not wait_until(
        deps,
        f"database {state.run_name}",
        deps.timeouts["database"],
        lambda: database_is_active(deps, state, holder),
    ):
        raise SuiteFailure(f"database {state.run_name} did not become active")
    state.database_id = str(holder["id"])
    deps.remember_secret(state.database_id)
    deps.log(f"database {state.run_name} cloud=active ready=true")


def database_is_active(deps, state: State, holder: dict) -> bool:
    code, rows = deps.cloud.list_databases(state.subscription_id)
    row = find_named(rows, f"{state.run_name}-db")
    status = "missing"
    if code == 404:
        status = "http-404"
    elif row is not None:
        status = str(row.get("status") or "missing")
    cr = deps.cluster.get(DATABASE, state.run_name)
    deps.log(f"database {state.run_name} cloud={status} ready={is_ready(cr)} reason={ready_reason(cr)}")
    if status == "error":
        raise SuiteFailure(f"database {state.run_name} cloud status error")
    if status in ("missing", "http-404") and "400" in ready_message(cr):
        raise SuiteFailure(f"database {state.run_name} was rejected by the API")
    cr_id = ((cr or {}).get("status") or {}).get("atProvider", {}).get("id")
    if status == "active" and is_ready(cr) and cr_id not in (None, ""):
        deps.remember_secret(str(cr_id))
        holder["id"] = observed_database_id(row, cr_id)
        return True
    return False


def delete_database(deps, state: State) -> int:
    deps.log(f"deleting database {state.database_name}")
    delete_owned(deps, state, DATABASE, state.database_name)
    code = wait_http_404(
        deps,
        f"database {state.database_name}",
        lambda: deps.cloud.get_database(state.subscription_id, state.database_id),
    )
    if code == 404 and not wait_until(deps, "database CR finalization", deps.timeouts["delete"],
                                      lambda: deps.cluster.get(DATABASE, state.database_name) is None):
        raise SuiteFailure("database CR still present; preserving parent")
    state.database_deleted = code == 404
    return code


def delete_subscription(deps, state: State) -> int:
    code, children = deps.cloud.list_databases_complete(state.subscription_id)
    if code != 200 or children:
        raise SuiteFailure("subscription child inventory is not verified empty; preserving parent")
    deps.log(f"deleting subscription {state.subscription_name}")
    delete_owned(deps, state, SUBSCRIPTION, state.subscription_name)
    code = wait_http_404(
        deps,
        f"subscription {state.subscription_name}",
        lambda: deps.cloud.get_subscription(state.subscription_id),
    )
    if code == 404 and not wait_until(deps, "subscription CR finalization", deps.timeouts["delete"],
                                      lambda: deps.cluster.get(SUBSCRIPTION, state.subscription_name) is None):
        raise SuiteFailure("subscription CR still present")
    state.subscription_deleted = code == 404
    return code


def wait_http_404(deps, label: str, getter) -> int:
    seen = {"code": 0}

    def gone() -> bool:
        code, _body = getter()
        seen["code"] = code
        deps.log(f"{label} HTTP {code}")
        return code == 404

    wait_until(deps, f"{label} HTTP 404", deps.timeouts["delete"], gone)
    return seen["code"]


def run_acl_role_and_user(deps, state: State) -> None:
    state.role_name = state.run_name
    create_owned(deps, state, ROLE, role_manifest(state.run_name, state.run_name, state.subscription_id, state.database_id))
    if not wait_until(
        deps,
        f"ACL role {state.run_name}",
        deps.timeouts["acl"],
        lambda: named_is_active(deps, ROLE, state.run_name, deps.cloud.list_acl_roles()),
    ):
        raise SuiteFailure(f"ACL role {state.run_name} did not become active")
    password = acl_password()
    deps.remember_secret(password)
    state.password_secret = f"{state.run_name}-acl-password"
    create_owned(deps, state, "secret", password_secret_manifest(state.password_secret, password), namespace="default")
    state.user_name = state.run_name
    create_owned(deps, state, USER, user_manifest(state.run_name, state.run_name, state.password_secret))
    if not wait_until(
        deps,
        f"ACL user {state.run_name}",
        deps.timeouts["acl"],
        lambda: named_is_active(deps, USER, state.run_name, deps.cloud.list_acl_users()),
    ):
        raise SuiteFailure(f"ACL user {state.run_name} did not become active")
    delete_owned(deps, state, USER, state.run_name)
    if not wait_until(
        deps,
        f"ACL user {state.run_name} delete",
        deps.timeouts["delete"],
        lambda: find_named(deps.cloud.list_acl_users(), state.run_name) is None,
    ):
        raise SuiteFailure(f"ACL user {state.run_name} is still present")
    delete_owned(deps, state, "secret", state.password_secret, namespace="default")
    state.password_secret = None
    state.user_name = None
    delete_owned(deps, state, ROLE, state.run_name)
    if not wait_until(
        deps,
        f"ACL role {state.run_name} delete",
        deps.timeouts["delete"],
        lambda: find_named(deps.cloud.list_acl_roles(), state.run_name) is None,
    ):
        raise SuiteFailure(f"ACL role {state.run_name} is still present")
    state.role_name = None
    deps.log(f"ACL role and user {state.run_name} created and gone")


def named_is_active(deps, resource: str, name: str, rows: list) -> bool:
    row = find_named(rows, name)
    status = str((row or {}).get("status") or "missing")
    cr = deps.cluster.get(resource, name)
    deps.log(f"{resource}/{name} ready={is_ready(cr)} cloud={status}")
    return is_ready(cr) and status == "active"


def strict_essentials(deps) -> None:
    """Qualify one GCP Essentials pair, without the shared-prefix cleanup path."""
    state = State(f"{PREFIX}{deps.run_id}")
    if deps.cloud.list_fixed_subscriptions_checked():
        raise SuiteFailure("account already has an essentials subscription; refusing to modify it")
    deps.cluster.install_provider_config()
    try:
        apply_lookups(deps)
        payment_id = str(deps.cluster.payment_method_id() or "").strip()
        plan_id = str(deps.cluster.plan_id() or "").strip()
        if not payment_id or not plan_id:
            raise SuiteFailure("payment or GCP Essentials plan lookup has no id")
        plan = deps.cluster.get(PLAN, f"{PREFIX}{deps.run_id}-plan")
        observed_plan = ((plan or {}).get("status") or {}).get("atProvider") or {}
        if (str(observed_plan.get("id") or "") != plan_id or
                str(observed_plan.get("cloudProvider") or "").upper() != "GCP" or
                str(observed_plan.get("region") or "").lower() != "us-central1"):
            raise SuiteFailure("looked-up plan is not the requested GCP region and ID")
        deps.log("verified Essentials plan provider GCP, region us-central1")
        deps.remember_secret(payment_id)
        deps.remember_secret(plan_id)
        state.essentials_name = state.run_name
        sub_manifest = essentials_subscription_manifest(state.run_name, plan_id, payment_id)
        sub_manifest["spec"]["deletionPolicy"] = "Delete"
        create_owned(deps, state, ESS_SUBSCRIPTION, sub_manifest)
        holder = {}
        if not wait_until(deps, "essentials Ready", deps.timeouts["subscription"],
                          lambda: essentials_is_active(deps, state.run_name, holder)):
            raise SuiteFailure("essentials subscription did not become active")
        state.essentials_id = str(holder["id"])
        deps.remember_secret(state.essentials_id)
        sub = deps.cluster.get(ESS_SUBSCRIPTION, state.run_name)
        cr_sub_id = strict_identity(sub, "subscription")
        if sub["metadata"]["uid"] != state.owned_uids.get((ESS_SUBSCRIPTION, state.run_name, None)):
            raise SuiteFailure("subscription CR was replaced")
        row = find_named(deps.cloud.list_fixed_subscriptions_checked(), state.run_name)
        if not row or str(row.get("id") or "") != cr_sub_id:
            raise SuiteFailure("subscription CR and Cloud identity differ")
        code, _ = deps.cloud.get_fixed_subscription(state.essentials_id)
        deps.log(f"subscription baseline {'HTTP_OK' if code == 200 else 'HTTP_ERROR'}; UID={sub['metadata']['uid']} finalizers={len(sub['metadata']['finalizers'])}")
        if code != 200:
            raise SuiteFailure("subscription baseline is not HTTP 200")
        state.essentials_database_name = state.run_name
        db_manifest = essentials_database_manifest(state.run_name, state.essentials_id)
        db_manifest["spec"]["deletionPolicy"] = "Delete"
        create_owned(deps, state, ESS_DATABASE, db_manifest)
        db_holder = {}
        if not wait_until(deps, "essentials database Ready", deps.timeouts["database"],
                          lambda: essentials_database_is_active(deps, state, db_holder)):
            raise SuiteFailure("essentials database did not become active")
        state.essentials_database_id = str(db_holder["id"])
        deps.remember_secret(state.essentials_database_id)
        db = deps.cluster.get(ESS_DATABASE, state.run_name)
        cr_db_id = strict_identity(db, "database")
        if db["metadata"]["uid"] != state.owned_uids.get((ESS_DATABASE, state.run_name, None)):
            raise SuiteFailure("database CR was replaced")
        if cr_db_id != f"{state.essentials_id}/{state.essentials_database_id}":
            raise SuiteFailure("database CR identity or parent differs")
        code, _ = deps.cloud.get_fixed_database(state.essentials_id, state.essentials_database_id)
        deps.log(f"database baseline {'HTTP_OK' if code == 200 else 'HTTP_ERROR'}; UID={db['metadata']['uid']} finalizers={len(db['metadata']['finalizers'])}")
        if code != 200:
            raise SuiteFailure("database baseline is not HTTP 200")
        deps.cluster.record_controller_evidence(state.run_name)

        qualify_essentials_eviction_update(deps, state, db)

        delete_owned(deps, state, ESS_DATABASE, state.run_name)
        def database_gone():
            parent_code, _ = deps.cloud.get_fixed_subscription(state.essentials_id)
            db_code, _ = deps.cloud.get_fixed_database(state.essentials_id, state.essentials_database_id)
            cr_gone = deps.cluster.get(ESS_DATABASE, state.run_name) is None
            deps.log(f"database delete {'HTTP_NOT_FOUND' if db_code == 404 else 'HTTP_OK' if db_code == 200 else 'HTTP_ERROR'}; parent {'HTTP_OK' if parent_code == 200 else 'HTTP_ERROR'}; CR gone={cr_gone}")
            if parent_code != 200:
                raise SuiteFailure("parent did not remain HTTP 200 during database deletion")
            if db_code not in (200, 404):
                raise SuiteFailure(f"database read HTTP {db_code}")
            return db_code == 404 and cr_gone
        if not wait_until(deps, "database deletion", deps.timeouts["delete"], database_gone):
            raise SuiteFailure("database deletion timed out")
        state.essentials_database_deleted = True
        deps.cluster.require_deleted_event(db["metadata"]["uid"], "database")
        _, db_inventory = deps.cloud.list_fixed_databases_complete(state.essentials_id)
        if db_inventory:
            raise SuiteFailure("database inventory is not empty; preserving parent")
        deps.log("authenticated database inventory complete; all child databases absent")
        delete_owned(deps, state, ESS_SUBSCRIPTION, state.run_name)
        def subscription_gone():
            code, _ = deps.cloud.get_fixed_subscription(state.essentials_id)
            cr_gone = deps.cluster.get(ESS_SUBSCRIPTION, state.run_name) is None
            deps.log(f"subscription delete {'HTTP_NOT_FOUND' if code == 404 else 'HTTP_OK' if code == 200 else 'HTTP_ERROR'}; CR gone={cr_gone}")
            if code not in (200, 404):
                raise SuiteFailure(f"subscription read HTTP {code}")
            return code == 404 and cr_gone
        if not wait_until(deps, "subscription deletion", deps.timeouts["delete"], subscription_gone):
            raise SuiteFailure("subscription deletion timed out")
        state.essentials_deleted = True
        deps.cluster.require_deleted_event(sub["metadata"]["uid"], "subscription")
        if find_named(deps.cloud.list_fixed_subscriptions_checked(), state.run_name):
            raise SuiteFailure("run-owned subscription remains in inventory")
        deps.log("strict essentials qualification passed; run-owned subscription inventory empty")
    except Exception:
        deps.log("strict qualification FAILED; retaining controller and credentials for CR recovery")
        deadline = deps.now() + deps.timeouts["recovery_hold"]
        deps.recovery_deadline = deadline
        while True:
            try:
                complete = strict_recover(deps, state)
            except Exception:
                complete = False
                deps.log("recovery encountered an error; residue is unknown")
            if complete:
                deps.log("recovery complete; original qualification remains FAILED")
                break
            if deps.now() >= deadline:
                deps.log("RECOVERY HOLD TIMED OUT: residue is unverified; use this Actions run and its encrypted ownership artifact for follow-up")
                break
            deps.log("RECOVERY HOLD: cleanup unresolved; retaining controller and credentials")
            deps.sleep(min(deps.recovery_poll, max(0, deadline - deps.now())))
        deps.recovery_deadline = None
        raise
    else:
        delete_lookups(deps)


def qualify_essentials_eviction_update(deps, state: State, baseline: dict) -> None:
    """Change one field without changing the exact names used by recovery."""
    code, body = deps.cloud.get_fixed_database(state.essentials_id, state.essentials_database_id)
    if (code != 200 or not isinstance(body, dict) or
            str(body.get("databaseId") or "") != state.essentials_database_id or
            body.get("name") != f"{state.run_name}-db"):
        raise SuiteFailure("eviction update baseline identity is unverified")
    before = body.get("dataEvictionPolicy")
    if before not in ("noeviction", "allkeys-lru", "allkeys-lfu", "allkeys-random",
                      "volatile-lru", "volatile-lfu", "volatile-random", "volatile-ttl"):
        raise SuiteFailure("eviction update baseline policy is unexpected")
    target = "noeviction" if before == "allkeys-lru" else "allkeys-lru"
    baseline_uid = baseline["metadata"]["uid"]
    if baseline_uid != state.owned_uids.get((ESS_DATABASE, state.run_name, None)):
        raise SuiteFailure("eviction update CR ownership is unverified")
    deps.cluster.replace_owned(ESS_DATABASE, state.run_name, baseline_uid, {"dataEviction": target})
    def converged():
        cr = deps.cluster.get(ESS_DATABASE, state.run_name)
        if not cr or (cr.get("metadata") or {}).get("uid") != baseline_uid:
            raise SuiteFailure("eviction update changed database CR identity")
        observed = ((cr.get("status") or {}).get("atProvider") or {})
        if str(observed.get("id") or "") != f"{state.essentials_id}/{state.essentials_database_id}":
            raise SuiteFailure("eviction update changed database Cloud identity")
        code, current = deps.cloud.get_fixed_database(state.essentials_id, state.essentials_database_id)
        if code != 200 or not isinstance(current, dict):
            raise SuiteFailure("eviction update Cloud read failed")
        if (str(current.get("databaseId") or "") != state.essentials_database_id or
                current.get("name") != f"{state.run_name}-db"):
            raise SuiteFailure("eviction update Cloud identity changed")
        return (strict_ready(cr) and observed.get("dataEviction") == target and
                current.get("dataEvictionPolicy") == target and current.get("status") == "active")
    if not wait_until(deps, "essentials eviction update", deps.timeouts["database"], converged):
        raise SuiteFailure("eviction update did not converge")
    deps.log(f"essentials eviction update verified {before} -> {target}; CR and Cloud IDs unchanged")
    deps.log("unchanged reconcile and no-extra-write evidence not established")


def strict_recover(deps, state: State) -> bool:
    """Recover only exact identities created by this run; never convert failure to pass."""
    refresh_recovery_identity(deps, state)
    if state.essentials_database_name and not state.essentials_database_deleted:
        if not state.essentials_id or not state.essentials_database_id:
            deps.log("recovery incomplete: database identity not established; an asynchronous create may still finish, so CR and parent are preserved")
            return False
        cr = deps.cluster.get(ESS_DATABASE, state.run_name)
        cr_id = str((((cr or {}).get("status") or {}).get("atProvider") or {}).get("id") or "")
        if cr is None and (ESS_DATABASE, state.run_name, None) not in state.deletion_requested:
            deps.log("recovery incomplete: database CR absent without confirmed delete")
            return False
        if cr is not None and cr_id != f"{state.essentials_id}/{state.essentials_database_id}":
            deps.log("recovery incomplete: database CR identity differs; CR and parent preserved")
            return False
        cloud_code, cloud_db = deps.cloud.get_fixed_database(state.essentials_id, state.essentials_database_id)
        if cloud_code not in (200, 404) or (cloud_code == 200 and
                (not isinstance(cloud_db, dict) or
                 observed_database_id(cloud_db, "") != state.essentials_database_id or
                 cloud_db.get("name") != f"{state.run_name}-db")):
            deps.log("recovery incomplete: database Cloud identity unverified; CR and parent preserved")
            return False
        if cr is not None:
            delete_owned(deps, state, ESS_DATABASE, state.run_name)
        code = wait_http_404(deps, "recovery database", lambda: deps.cloud.get_fixed_database(
            state.essentials_id, state.essentials_database_id))
        if code != 404:
            deps.log("recovery incomplete: database remains or cannot be read; parent preserved")
            return False
        if not wait_until(deps, "recovery database CR finalization", deps.timeouts["delete"],
                          lambda: deps.cluster.get(ESS_DATABASE, state.run_name) is None):
            deps.log("recovery incomplete: database CR remains; parent preserved")
            return False
    if state.essentials_name and not state.essentials_deleted:
        if not state.essentials_id:
            deps.log("recovery incomplete: subscription identity not established; CR preserved")
            return False
        cr = deps.cluster.get(ESS_SUBSCRIPTION, state.run_name)
        cr_id = str((((cr or {}).get("status") or {}).get("atProvider") or {}).get("id") or "")
        if cr is None and (ESS_SUBSCRIPTION, state.run_name, None) not in state.deletion_requested:
            deps.log("recovery incomplete: subscription CR absent without confirmed delete")
            return False
        if cr is not None and cr_id != state.essentials_id:
            deps.log("recovery incomplete: subscription CR identity differs; CR preserved")
            return False
        parent_code, _ = deps.cloud.get_fixed_subscription(state.essentials_id)
        if parent_code not in (200, 404):
            deps.log("recovery incomplete: parent existence and ownership unverified")
            return False
        if parent_code == 404 and cr is not None:
            deps.log("recovery incomplete: parent is absent but CR remains; child inventory cannot be verified")
            return False
        if parent_code == 200:
            if not owned_essentials_subscription(deps, state):
                deps.log("recovery incomplete: parent ownership unverified")
                return False
            list_code, rows = deps.cloud.list_fixed_databases_complete(state.essentials_id)
            if list_code != 200 or rows:
                deps.log("recovery incomplete: child inventory is not completely empty")
                return False
        if cr is not None:
            delete_owned(deps, state, ESS_SUBSCRIPTION, state.run_name)
        if state.essentials_id:
            code = wait_http_404(deps, "recovery subscription", lambda: deps.cloud.get_fixed_subscription(state.essentials_id))
            if code != 404:
                deps.log("recovery incomplete: subscription remains or cannot be read")
                return False
        else:
            deps.log("recovery incomplete: subscription identity not established; absence unverified")
            return False
    if find_named(deps.cloud.list_fixed_subscriptions_checked(), state.run_name):
        deps.log("recovery incomplete: run-owned subscription remains in inventory")
        return False
    for resource, name in ((ESS_DATABASE, state.essentials_database_name),
                           (ESS_SUBSCRIPTION, state.essentials_name)):
        if name and not wait_until(deps, "recovery CR finalization", deps.timeouts["delete"],
                                   lambda resource=resource, name=name: deps.cluster.get(resource, name) is None):
            deps.log("recovery incomplete: CR remains; preserving provider credentials")
            return False
    deps.log("recovery: no run-owned subscription found")
    delete_lookups(deps)
    return True


def refresh_recovery_identity(deps, state: State) -> None:
    """Bind only a CR identity independently matched to this run's Cloud name."""
    if state.subscription_name and not state.subscription_id:
        cr = deps.cluster.get(SUBSCRIPTION, state.subscription_name)
        cr_id = str((((cr or {}).get("status") or {}).get("atProvider") or {}).get("id") or "")
        row = find_named(deps.cloud.list_subscriptions(), state.run_name)
        if cr_id and row and str(row.get("id") or "") == cr_id:
            state.subscription_id = cr_id
            deps.remember_secret(cr_id)
            deps.log("recovery matched Pro subscription identity")
    if state.subscription_id and state.database_name and not state.database_id:
        cr = deps.cluster.get(DATABASE, state.database_name)
        cr_id = str((((cr or {}).get("status") or {}).get("atProvider") or {}).get("id") or "")
        code, rows = deps.cloud.list_databases_complete(state.subscription_id)
        matches = [row for row in rows if isinstance(row, dict) and row.get("name") == f"{state.run_name}-db"]
        row = matches[0] if len(matches) == 1 else None
        if code == 200 and row and cr_id:
            db_id = observed_database_id(row, "")
            if db_id and cr_id == f"{state.subscription_id}/{db_id}":
                state.database_id = db_id
                deps.remember_secret(cr_id)
                deps.remember_secret(db_id)
                deps.log("recovery matched Pro database and parent identity")
    if state.essentials_name and not state.essentials_id:
        cr = deps.cluster.get(ESS_SUBSCRIPTION, state.run_name)
        cr_id = str((((cr or {}).get("status") or {}).get("atProvider") or {}).get("id") or "")
        row = find_named(deps.cloud.list_fixed_subscriptions_checked(), state.run_name)
        if cr_id and row and str(row.get("id") or "") == cr_id:
            state.essentials_id = cr_id
            deps.remember_secret(cr_id)
            deps.log("recovery matched subscription identity")
    if (state.essentials_id and state.essentials_database_name and not state.essentials_database_deleted
            and not state.essentials_database_id):
        cr = deps.cluster.get(ESS_DATABASE, state.run_name)
        cr_id = str((((cr or {}).get("status") or {}).get("atProvider") or {}).get("id") or "")
        code, rows = deps.cloud.list_fixed_databases_complete(state.essentials_id)
        matches = [row for row in rows if isinstance(row, dict) and row.get("name") == f"{state.run_name}-db"]
        row = matches[0] if len(matches) == 1 else None
        if code == 200 and row and cr_id:
            db_id = observed_database_id(row, "")
            if cr_id == f"{state.essentials_id}/{db_id}":
                state.essentials_database_id = db_id
                deps.remember_secret(cr_id)
                deps.remember_secret(db_id)
                deps.log("recovery matched database and parent identity")


def owned_essentials_subscription(deps, state: State) -> bool:
    rows = [row for row in deps.cloud.list_fixed_subscriptions_checked()
            if str(row.get("name") or "") == state.run_name]
    return len(rows) == 1 and str(rows[0].get("id") or "") == state.essentials_id


def run_essentials(deps, state: State, payment_id: str) -> None:
    existing = deps.cloud.list_fixed_subscriptions()
    if existing:
        raise SuiteFailure("account already has an essentials subscription")
    plan_id = str(deps.cluster.plan_id() or "").strip()
    if not plan_id:
        raise SuiteFailure("essentials plan lookup has no id")
    deps.remember_secret(plan_id)
    state.essentials_name = state.run_name
    create_owned(deps, state, ESS_SUBSCRIPTION, essentials_subscription_manifest(state.run_name, plan_id, payment_id))
    holder = {}
    if not wait_until(
        deps,
        f"essentials {state.run_name}",
        deps.timeouts["subscription"],
        lambda: essentials_is_active(deps, state.run_name, holder),
    ):
        raise SuiteFailure(f"essentials {state.run_name} did not become active")
    state.essentials_id = str(holder["id"])
    deps.remember_secret(state.essentials_id)
    state.essentials_database_name = state.run_name
    create_owned(deps, state, ESS_DATABASE, essentials_database_manifest(state.run_name, state.essentials_id))
    db_holder = {}
    if not wait_until(
        deps,
        f"essentials database {state.run_name}",
        deps.timeouts["database"],
        lambda: essentials_database_is_active(deps, state, db_holder),
    ):
        raise SuiteFailure(f"essentials database {state.run_name} did not become active")
    state.essentials_database_id = str(db_holder["id"])
    deps.remember_secret(state.essentials_database_id)
    delete_owned(deps, state, ESS_DATABASE, state.run_name)
    database_code = wait_http_404(
        deps,
        f"essentials database {state.run_name}",
        lambda: deps.cloud.get_fixed_database(state.essentials_id, state.essentials_database_id),
    )
    state.essentials_database_deleted = database_code == 404
    if database_code != 404:
        raise SuiteFailure("essentials database absence unverified; preserving parent")
    if not wait_until(deps, "essentials database CR finalization", deps.timeouts["delete"],
                      lambda: deps.cluster.get(ESS_DATABASE, state.run_name) is None):
        raise SuiteFailure("essentials database CR still present; preserving parent")
    list_code, children = deps.cloud.list_fixed_databases_complete(state.essentials_id)
    if list_code != 200 or children:
        raise SuiteFailure("essentials child inventory is not verified empty; preserving parent")
    delete_owned(deps, state, ESS_SUBSCRIPTION, state.run_name)
    subscription_code = wait_http_404(
        deps,
        f"essentials {state.run_name}",
        lambda: deps.cloud.get_fixed_subscription(state.essentials_id),
    )
    state.essentials_deleted = subscription_code == 404
    if subscription_code == 404 and not wait_until(
            deps, "essentials subscription CR finalization", deps.timeouts["delete"],
            lambda: deps.cluster.get(ESS_SUBSCRIPTION, state.run_name) is None):
        raise SuiteFailure("essentials subscription CR still present")
    if database_code != 404 or subscription_code != 404:
        raise SuiteFailure(
            f"essentials delete HTTP database {database_code} subscription {subscription_code}"
        )
    deps.log(f"essentials {state.run_name} created and gone")


def essentials_is_active(deps, name: str, holder: dict) -> bool:
    row = find_named(deps.cloud.list_fixed_subscriptions(), name)
    status = str((row or {}).get("status") or "missing")
    cr = deps.cluster.get(ESS_SUBSCRIPTION, name)
    deps.log(f"essentials {name} cloud={status} ready={is_ready(cr)} reason={ready_reason(cr)}")
    if status == "error":
        raise SuiteFailure(f"essentials {name} cloud status error")
    cr_id = ((cr or {}).get("status") or {}).get("atProvider", {}).get("id")
    if status == "active" and is_ready(cr) and cr_id not in (None, ""):
        holder["id"] = cr_id
        return True
    return False


def essentials_database_is_active(deps, state: State, holder: dict) -> bool:
    code, rows = deps.cloud.list_fixed_databases(state.essentials_id)
    row = find_named(rows, f"{state.run_name}-db")
    status = "http-404" if code == 404 else str((row or {}).get("status") or "missing")
    cr = deps.cluster.get(ESS_DATABASE, state.run_name)
    deps.log(
        f"essentials database {state.run_name} cloud={status} "
        f"ready={is_ready(cr)} reason={ready_reason(cr)}"
    )
    if status == "error":
        raise SuiteFailure(f"essentials database {state.run_name} cloud status error")
    cr_id = ((cr or {}).get("status") or {}).get("atProvider", {}).get("id")
    if status == "active" and is_ready(cr) and cr_id not in (None, ""):
        deps.remember_secret(str(cr_id))
        holder["id"] = observed_database_id(row, cr_id)
        return True
    return False


def cleanup(deps, state: State) -> bool:
    """CR-only recovery of this run, preserving parents on unknown child state."""
    refresh_recovery_identity(deps, state)
    def named(resource, name, list_rows):
        if not name:
            return True
        cr = deps.cluster.get(resource, name)
        row = find_named(list_rows(), name)
        if cr is None and row is None and (resource, name) in state.named_deletion_requested:
            return True
        cr_id = str((((cr or {}).get("status") or {}).get("atProvider") or {}).get("id") or "")
        if not cr or not row or not cr_id or str(row.get("id") or "") != cr_id:
            deps.log("recovery named-resource identity unknown; preserving credentials")
            return False
        delete_owned(deps, state, resource, name)
        state.named_deletion_requested.add((resource, name))
        return wait_until(deps, "recovery named resource", deps.timeouts["delete"],
                          lambda: deps.cluster.get(resource, name) is None and
                          find_named(list_rows(), name) is None)

    if not named(USER, state.user_name, deps.cloud.list_acl_users):
        return False
    state.user_name = None
    if state.password_secret:
        delete_owned(deps, state, "secret", state.password_secret, namespace="default")
        state.password_secret = None
    if not named(ROLE, state.role_name, deps.cloud.list_acl_roles):
        return False
    state.role_name = None

    def child(resource, name, parent_id, child_id, getter):
        if not name:
            return True
        if not parent_id or not child_id:
            deps.log("recovery child identity unknown; parent preserved")
            return False
        cr = deps.cluster.get(resource, name)
        code, body = getter(parent_id, child_id)
        if code == 404 and cr is None:
            return (resource, name, None) in state.deletion_requested
        cr_id = str((((cr or {}).get("status") or {}).get("atProvider") or {}).get("id") or "")
        if cr_id != f"{parent_id}/{child_id}" or code not in (200, 404):
            deps.log("recovery child identity or Cloud read unverified; parent preserved")
            return False
        if code == 200 and (not isinstance(body, dict) or
                observed_database_id(body, "") != child_id or body.get("name") != f"{state.run_name}-db"):
            deps.log("recovery child Cloud identity mismatched; parent preserved")
            return False
        delete_owned(deps, state, resource, name)
        def gone():
            code, _ = getter(parent_id, child_id)
            if code not in (200, 404):
                raise SuiteFailure("recovery child Cloud read failed")
            return code == 404 and deps.cluster.get(resource, name) is None
        return wait_until(deps, "recovery child absence", deps.timeouts["delete"], gone)

    def parent(resource, name, parent_id, getter, list_children):
        if not name:
            return True
        if not parent_id:
            deps.log("recovery parent identity unknown; CR preserved")
            return False
        cr = deps.cluster.get(resource, name)
        code, body = getter(parent_id)
        if code == 404 and cr is None:
            return (resource, name, None) in state.deletion_requested
        if code == 404:
            deps.log("recovery parent absent but CR remains; child inventory cannot be verified")
            return False
        cr_id = str((((cr or {}).get("status") or {}).get("atProvider") or {}).get("id") or "")
        if cr_id != parent_id or code not in (200, 404):
            deps.log("recovery parent identity or Cloud read unverified")
            return False
        if code == 200 and (not isinstance(body, dict) or
                str(body.get("id") or "") != parent_id or body.get("name") != state.run_name):
            deps.log("recovery parent Cloud identity mismatched")
            return False
        if code == 200:
            list_code, children = list_children(parent_id)
            if list_code != 200 or children:
                deps.log("recovery child inventory unverified or nonempty; parent preserved")
                return False
        delete_owned(deps, state, resource, name)
        def gone():
            code, _ = getter(parent_id)
            if code not in (200, 404):
                raise SuiteFailure("recovery parent Cloud read failed")
            return code == 404 and deps.cluster.get(resource, name) is None
        return wait_until(deps, "recovery parent absence", deps.timeouts["delete"], gone)

    if not child(ESS_DATABASE, state.essentials_database_name, state.essentials_id,
                 state.essentials_database_id, deps.cloud.get_fixed_database):
        return False
    state.essentials_database_name = None
    if not parent(ESS_SUBSCRIPTION, state.essentials_name, state.essentials_id,
                  deps.cloud.get_fixed_subscription, deps.cloud.list_fixed_databases_complete):
        return False
    state.essentials_name = None
    if not child(DATABASE, state.database_name, state.subscription_id,
                 state.database_id, deps.cloud.get_database):
        return False
    state.database_name = None
    if not parent(SUBSCRIPTION, state.subscription_name, state.subscription_id,
                  deps.cloud.get_subscription, deps.cloud.list_databases_complete):
        return False
    state.subscription_name = None
    if not named(ACL, state.acl_name, deps.cloud.list_acl_rules):
        return False
    state.acl_name = None
    deps.log("standard CR-only recovery verified; original qualification remains failed")
    return True


def wait_until(deps, label: str, timeout: float, ready) -> bool:
    deadline = deps.now() + timeout
    if deps.recovery_deadline is not None:
        deadline = min(deadline, deps.recovery_deadline)
    while True:
        if ready():
            return True
        if deps.now() >= deadline:
            deps.log(f"timed out waiting for {label}")
            return False
        deps.sleep(deps.poll)


def find_named(rows: list, name: str):
    for row in rows:
        if str(row.get("name") or "") == name:
            return row
    return None


def apply_lookups(deps) -> None:
    deps.lookups_applied = True
    run_id = deps.run_id
    for resource, manifest in zip((PAYMENT, PLAN, REGIONS), lookup_manifests(run_id)):
        create_owned(deps, deps.aux_state, resource, manifest)
    refs = (
        (PAYMENT, f"{PREFIX}{run_id}-payment"),
        (PLAN, f"{PREFIX}{run_id}-plan"),
        (REGIONS, f"{PREFIX}{run_id}-regions"),
    )
    for resource, name in refs:
        if not wait_until(
            deps,
            f"{resource}/{name}",
            deps.timeouts["lookup"],
            lambda resource=resource, name=name: lookup_ready(deps, resource, name),
        ):
            raise SuiteFailure(f"{resource}/{name} did not become Ready")
    deps.lookups_applied = True
    deps.log("lookups Ready")


def lookup_ready(deps, resource: str, name: str) -> bool:
    cr = deps.cluster.get(resource, name)
    deps.log(f"{resource}/{name} ready={is_ready(cr)} reason={ready_reason(cr)}")
    return is_ready(cr)


def delete_lookups(deps) -> None:
    if not getattr(deps, "lookups_applied", False):
        return
    run_id = deps.run_id
    def remove_aux(resource, name, namespace=None):
        key = (resource, name, namespace)
        if key not in deps.aux_state.owned_uids:
            return
        if deps.cluster.get(resource, name, namespace=namespace) is None:
            raise SuiteFailure("auxiliary resource absent without a confirmed delete")
        delete_owned(deps, deps.aux_state, resource, name, namespace)
        # Remove only after a confirmed UID-preconditioned API response.
        deps.aux_state.owned_uids.pop(key)
    for resource, suffix in (
        (PAYMENT, "payment"),
        (PLAN, "plan"),
        (REGIONS, "regions"),
    ):
        name = f"{PREFIX}{run_id}-{suffix}"
        remove_aux(resource, name)
    remove_aux(PROVIDER_CONFIG, "default")
    remove_aux("secret", "provider-secret", namespace="upbound-system")


def acl_manifest(name: str) -> dict:
    return {
        "apiVersion": "acl.redis.io/v1beta1",
        "kind": "Rule",
        "metadata": {"name": name},
        "spec": {
            "forProvider": {"name": name, "rule": ACL_RULE},
            "providerConfigRef": {"name": "default"},
        },
    }


def subscription_manifest(name: str, payment_id: str, cidr: str) -> dict:
    return {
        "apiVersion": "rediscloud.redis.io/v1beta1",
        "kind": "Subscription",
        "metadata": {"name": name},
        "spec": {
            "forProvider": {
                "name": name,
                "memoryStorage": "ram",
                "paymentMethodId": payment_id,
                "cloudProvider": [
                    {
                        "provider": "AWS",
                        "region": [
                            {
                                "region": "us-east-1",
                                "networkingDeploymentCidr": cidr,
                                "multipleAvailabilityZones": True,
                            }
                        ],
                    }
                ],
                "creationPlan": [
                    {
                        "quantity": 1,
                        "replication": True,
                        "datasetSizeInGb": 1,
                        "throughputMeasurementBy": "operations-per-second",
                        "throughputMeasurementValue": 1000,
                    }
                ],
            },
            "providerConfigRef": {"name": "default"},
        },
    }


def role_manifest(name: str, rule_name: str, subscription_id: str, database_id: str) -> dict:
    return {
        "apiVersion": "acl.redis.io/v1beta1",
        "kind": "Role",
        "metadata": {"name": name},
        "spec": {
            "forProvider": {
                "name": name,
                "rule": [
                    {
                        "name": rule_name,
                        "database": [
                            {
                                "subscription": numeric_id(subscription_id),
                                "database": numeric_id(database_id),
                            }
                        ],
                    }
                ],
            },
            "providerConfigRef": {"name": "default"},
        },
    }


def user_manifest(name: str, role_name: str, secret_name: str) -> dict:
    return {
        "apiVersion": "acl.redis.io/v1beta1",
        "kind": "User",
        "metadata": {"name": name},
        "spec": {
            "forProvider": {
                "name": name,
                "role": role_name,
                "passwordSecretRef": {
                    "name": secret_name,
                    "namespace": "default",
                    "key": "password",
                },
            },
            "providerConfigRef": {"name": "default"},
        },
    }


def password_secret_manifest(name: str, password: str) -> dict:
    return {
        "apiVersion": "v1",
        "kind": "Secret",
        "metadata": {"name": name, "namespace": "default"},
        "stringData": {"password": password},
    }


def acl_password() -> str:
    alphabet = string.ascii_letters + string.digits
    chars = [
        secrets.choice(string.ascii_lowercase),
        secrets.choice(string.ascii_uppercase),
        secrets.choice(string.digits),
        secrets.choice("!@#%^*-_=+"),
    ]
    chars.extend(secrets.choice(alphabet) for _ in range(16))
    secrets.SystemRandom().shuffle(chars)
    return "".join(chars)


def essentials_subscription_manifest(name: str, plan_id: str, payment_id: str) -> dict:
    return {
        "apiVersion": "essentials.redis.io/v1beta1",
        "kind": "Subscription",
        "metadata": {"name": name},
        "spec": {
            "forProvider": {
                "name": name,
                "planId": numeric_id(plan_id),
                "paymentMethod": "credit-card",
                "paymentMethodId": numeric_id(payment_id),
            },
            "providerConfigRef": {"name": "default"},
        },
    }


def essentials_database_manifest(name: str, subscription_id: str) -> dict:
    return {
        "apiVersion": "essentials.redis.io/v1beta1",
        "kind": "Database",
        "metadata": {"name": name},
        "spec": {
            "forProvider": {
                "name": f"{name}-db",
                "subscriptionId": numeric_id(subscription_id),
                "dataPersistence": "none",
                "replication": False,
            },
            "providerConfigRef": {"name": "default"},
        },
    }


def database_manifest(name: str, subscription_id: str) -> dict:
    return {
        "apiVersion": "subscription.redis.io/v1beta1",
        "kind": "Database",
        "metadata": {"name": name},
        "spec": {
            "forProvider": {
                "subscriptionId": numeric_id(subscription_id),
                "name": f"{name}-db",
                "protocol": "redis",
                "memoryLimitInGb": 0.1,
                "dataPersistence": "none",
                "throughputMeasurementBy": "operations-per-second",
                "throughputMeasurementValue": 1000,
                "replication": True,
            },
            "writeConnectionSecretToRef": {"name": name, "namespace": "default"},
            "providerConfigRef": {"name": "default"},
        },
    }


def lookup_manifests(run_id: str) -> list[dict]:
    pay = f"{PREFIX}{run_id}-payment"
    plan = f"{PREFIX}{run_id}-plan"
    regions = f"{PREFIX}{run_id}-regions"
    return [
        {
            "apiVersion": "lookup.redis.io/v1alpha1",
            "kind": "PaymentMethod",
            "metadata": {"name": pay},
            "spec": {
                "providerConfigRef": {"name": "default"},
                "forProvider": {"excludeExpired": True},
            },
        },
        {
            "apiVersion": "lookup.redis.io/v1alpha1",
            "kind": "EssentialsPlan",
            "metadata": {"name": plan},
            "spec": {
                "providerConfigRef": {"name": "default"},
                "forProvider": {
                    "cloudProvider": "GCP",
                    "region": "us-central1",
                    "name": "1GB",
                    "size": "1",
                    "sizeMeasurementUnit": "GB",
                    "supportReplication": False,
                    "supportDataPersistence": False,
                },
            },
        },
        {
            "apiVersion": "lookup.redis.io/v1alpha1",
            "kind": "Regions",
            "metadata": {"name": regions},
            "spec": {
                "providerConfigRef": {"name": "default"},
                "forProvider": {"providerName": "GCP", "name": "us-central1"},
            },
        },
    ]


def provider_config_manifests(credentials_json: str) -> list[dict]:
    return [
        {
            "apiVersion": "v1",
            "kind": "Secret",
            "metadata": {"name": "provider-secret", "namespace": "upbound-system"},
            "stringData": {"credentials": credentials_json},
        },
        {
            "apiVersion": "rediscloud.redis.io/v1beta1",
            "kind": "ProviderConfig",
            "metadata": {"name": "default"},
            "spec": {
                "credentials": {
                    "source": "Secret",
                    "secretRef": {
                        "name": "provider-secret",
                        "namespace": "upbound-system",
                        "key": "credentials",
                    },
                }
            },
        },
    ]


class Deps:
    def __init__(self, cloud, cluster, run_id: str, secrets: list[str] | None = None):
        self.cloud = cloud
        self.cluster = cluster
        self.run_id = run_id
        self.secrets = list(secrets or [])
        self.lines = []
        self.poll = 30.0
        self.recovery_poll = 60.0
        self.recovery_deadline = None
        self.timeouts = {
            "lookup": 300.0,
            "acl": 300.0,
            "subscription": 1800.0,
            "database": 1200.0,
            "delete": 900.0,
            "recovery_hold": 3600.0,
        }
        self.lookups_applied = False
        self.aux_state = State(f"{PREFIX}{run_id}")
        self._now = time.monotonic
        self._sleep = time.sleep

    def now(self) -> float:
        return self._now()

    def sleep(self, seconds: float) -> None:
        self._sleep(seconds)

    def remember_secret(self, value: str) -> None:
        if value and value not in self.secrets:
            self.secrets.append(value)

    def log(self, message: str) -> None:
        safe = redact(message, self.secrets)
        self.lines.append(safe)
        print(safe, flush=True)


class RefuseRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        return None


class RedisCloud:
    def __init__(self, base_url: str, api_key: str, secret_key: str):
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self.secret_key = secret_key

    def request(self, method: str, path: str):
        url = self.base_url + path
        req = urllib.request.Request(url, method=method)
        req.add_header("Accept", "application/json")
        req.add_header("X-Api-Key", self.api_key)
        req.add_header("X-Api-Secret-Key", self.secret_key)
        try:
            # Do not forward authenticated reads or writes to a redirected URL.
            with urllib.request.build_opener(RefuseRedirect()).open(req, timeout=60) as resp:
                raw = resp.read(MAX_HTTP_BODY_BYTES + 1)
                if len(raw) > MAX_HTTP_BODY_BYTES:
                    raise SuiteFailure("Cloud response exceeds the safety limit")
                body = json.loads(raw.decode()) if raw else {}
                return resp.status, body
        except urllib.error.HTTPError as err:
            err.close()
            return err.code, None
        except (OSError, http.client.HTTPException, json.JSONDecodeError):
            raise SuiteFailure("Cloud request failed or returned invalid JSON") from None

    def list_subscriptions(self) -> list:
        code, body = self.request("GET", "/subscriptions")
        if code != 200:
            raise SuiteFailure(f"list subscriptions HTTP {code}")
        return validated_account_rows(body, "subscriptions")

    def delete_subscription(self, sub_id: str) -> None:
        code, _body = self.request("DELETE", f"/subscriptions/{sub_id}")
        if code not in (200, 202, 204, 404):
            raise SuiteFailure(f"delete subscription HTTP {code}")

    def get_subscription(self, sub_id: str):
        return self.request("GET", f"/subscriptions/{sub_id}")

    def list_databases(self, sub_id: str):
        code, body = self.request("GET", f"/subscriptions/{sub_id}/databases")
        if code == 404:
            return code, []
        if code != 200:
            raise SuiteFailure(f"list databases HTTP {code}")
        return code, database_rows(body)

    def list_databases_complete(self, sub_id: str):
        # The official Go iterator uses limit/offset and checks the returned
        # subscription ID. A 404 or malformed page is never an empty inventory.
        rows, seen, totals, metadata = [], set(), set(), {}
        limit = 100
        for offset in range(0, 10000, limit):
            code, body = self.request("GET", f"/subscriptions/{sub_id}/databases?limit={limit}&offset={offset}")
            if code != 200:
                raise SuiteFailure(f"list databases page HTTP {code}")
            subscriptions = body.get("subscription") if isinstance(body, dict) else None
            if (not isinstance(subscriptions, list) or len(subscriptions) != 1 or
                    not isinstance(subscriptions[0], dict) or
                    str(subscriptions[0].get("subscriptionId") or "") != str(sub_id) or
                    not isinstance(subscriptions[0].get("databases"), list)):
                raise SuiteFailure("list databases page has unexpected shape or parent")
            if not validated_database_page(body, subscriptions[0], rows, seen, offset, limit, totals, metadata, "database"):
                return 200, rows
        raise SuiteFailure("list databases pagination did not terminate")

    def get_database(self, sub_id: str, db_id: str):
        return self.request("GET", f"/subscriptions/{sub_id}/databases/{db_id}")

    def delete_database(self, sub_id: str, db_id: str) -> None:
        self._delete(f"/subscriptions/{sub_id}/databases/{db_id}", "delete database")

    def list_acl_rules(self) -> list:
        return self._rows("GET", "/acl/redisRules", "list ACL rules", lambda body: validated_account_rows(body, "redisRules"))

    def delete_acl_rule(self, rule_id: str) -> None:
        self._delete(f"/acl/redisRules/{rule_id}", "delete ACL rule")

    def list_acl_roles(self) -> list:
        return self._rows("GET", "/acl/roles", "list ACL roles", lambda body: validated_account_rows(body, "roles"))

    def delete_acl_role(self, role_id: str) -> None:
        self._delete(f"/acl/roles/{role_id}", "delete ACL role")

    def list_acl_users(self) -> list:
        return self._rows("GET", "/acl/users", "list ACL users", lambda body: validated_account_rows(body, "users"))

    def delete_acl_user(self, user_id: str) -> None:
        self._delete(f"/acl/users/{user_id}", "delete ACL user")

    def list_fixed_subscriptions(self) -> list:
        return self._rows(
            "GET",
            "/fixed/subscriptions",
            "list essentials subscriptions",
            lambda body: validated_account_rows(body, "subscriptions"),
        )

    def list_fixed_subscriptions_checked(self) -> list:
        # The fixed subscription API returns the complete account list without
        # pagination, unlike its database endpoint.
        code, body = self.request("GET", "/fixed/subscriptions")
        if code != 200:
            raise SuiteFailure(f"list essentials subscriptions HTTP {code}")
        return validated_account_rows(body, "subscriptions")

    def delete_fixed_subscription(self, sub_id: str) -> None:
        self._delete(f"/fixed/subscriptions/{sub_id}", "delete essentials subscription")

    def get_fixed_subscription(self, sub_id: str):
        return self.request("GET", f"/fixed/subscriptions/{sub_id}")

    def list_fixed_databases(self, sub_id: str):
        code, body = self.request("GET", f"/fixed/subscriptions/{sub_id}/databases")
        if code == 404:
            return code, []
        if code != 200:
            raise SuiteFailure(f"list essentials databases HTTP {code}")
        return code, fixed_database_rows(body)

    def list_fixed_databases_complete(self, sub_id: str):
        # rediscloud-go-api's fixed database iterator advances by limit/offset
        # until an authenticated page is empty. Never infer absence from 404.
        rows, seen, totals, metadata = [], set(), set(), {}
        limit = 100
        for offset in range(0, 10000, limit):
            code, body = self.request("GET", f"/fixed/subscriptions/{sub_id}/databases?limit={limit}&offset={offset}")
            if code != 200:
                raise SuiteFailure(f"list essentials databases page HTTP {code}")
            if (not isinstance(body, dict) or not isinstance(body.get("subscription"), dict) or
                    str(body["subscription"].get("subscriptionId") or "") != str(sub_id) or
                    not isinstance(body["subscription"].get("databases"), list)):
                raise SuiteFailure("list essentials databases page has unexpected shape")
            if not validated_database_page(body, body["subscription"], rows, seen, offset, limit, totals, metadata, "essentials database"):
                return 200, rows
        raise SuiteFailure("list essentials databases pagination did not terminate")

    def get_fixed_database(self, sub_id: str, db_id: str):
        return self.request("GET", f"/fixed/subscriptions/{sub_id}/databases/{db_id}")

    def delete_fixed_database(self, sub_id: str, db_id: str) -> None:
        self._delete(
            f"/fixed/subscriptions/{sub_id}/databases/{db_id}",
            "delete essentials database",
        )

    def _rows(self, method: str, path: str, label: str, parse) -> list:
        code, body = self.request(method, path)
        if code != 200:
            raise SuiteFailure(f"{label} HTTP {code}")
        return parse(body)

    def _delete(self, path: str, label: str) -> None:
        code, _body = self.request("DELETE", path)
        if code not in (200, 202, 204, 404):
            raise SuiteFailure(f"{label} HTTP {code}")


class KubectlCluster:
    def __init__(self, kubectl: str, context: str, credentials: dict, deps: Deps):
        self.kubectl = kubectl
        self.context = context
        self.credentials = credentials
        self.deps = deps

    def install_provider_config(self) -> None:
        payload = json.dumps(self.credentials)
        self.deps.remember_secret(self.credentials["api_key"])
        self.deps.remember_secret(self.credentials["secret_key"])
        if self.credentials.get("url"):
            self.deps.remember_secret(self.credentials["url"])
        manifests = provider_config_manifests(payload)
        create_owned(self.deps, self.deps.aux_state, "secret", manifests[0], namespace="upbound-system")
        create_owned(self.deps, self.deps.aux_state, PROVIDER_CONFIG, manifests[1])
        self.deps.log("provider config installed")

    def apply(self, manifest: dict) -> None:
        self._run(["apply", "-f", "-"], stdin=json.dumps(manifest))
        name = manifest.get("metadata", {}).get("name", "")
        self.deps.log(f"applied {manifest.get('kind')} {name}")

    def create_owned(self, manifest: dict) -> str:
        proc = self._run(["create", "-f", "-", "-o", "json"], stdin=json.dumps(manifest))
        try:
            created = json.loads(proc.stdout)
            meta = created.get("metadata") or {}
            if (created.get("kind") != manifest.get("kind") or
                    created.get("apiVersion") != manifest.get("apiVersion") or
                    meta.get("name") != manifest.get("metadata", {}).get("name") or
                    (manifest.get("metadata", {}).get("namespace") is not None and
                     meta.get("namespace") != manifest["metadata"]["namespace"]) or
                    not isinstance(meta.get("uid"), str) or not meta["uid"]):
                raise ValueError("identity mismatch")
            return meta["uid"]
        except (ValueError, TypeError, AttributeError):
            raise SuiteFailure("created resource identity unavailable") from None

    def delete_owned(self, resource: str, name: str, uid: str, namespace: str | None = None) -> None:
        paths = {
            SUBSCRIPTION: "/apis/rediscloud.redis.io/v1beta1/subscriptions/",
            DATABASE: "/apis/subscription.redis.io/v1beta1/databases/",
            ESS_SUBSCRIPTION: "/apis/essentials.redis.io/v1beta1/subscriptions/",
            ESS_DATABASE: "/apis/essentials.redis.io/v1beta1/databases/",
            ACL: "/apis/acl.redis.io/v1beta1/rules/",
            ROLE: "/apis/acl.redis.io/v1beta1/roles/",
            USER: "/apis/acl.redis.io/v1beta1/users/",
            PAYMENT: "/apis/lookup.redis.io/v1alpha1/paymentmethods/",
            PLAN: "/apis/lookup.redis.io/v1alpha1/essentialsplans/",
            REGIONS: "/apis/lookup.redis.io/v1alpha1/regions/",
            PROVIDER_CONFIG: "/apis/rediscloud.redis.io/v1beta1/providerconfigs/",
        }
        if not re.fullmatch(r"[a-z0-9]([-a-z0-9]*[a-z0-9])?", name) or not uid:
            raise SuiteFailure("invalid owned resource identity")
        if resource == "secret" and namespace in ("default", "upbound-system"):
            path = f"/api/v1/namespaces/{namespace}/secrets/" + name
        elif namespace is None and resource in paths:
            path = paths[resource] + name
        else:
            raise SuiteFailure("unsupported owned resource")
        options = {"apiVersion": "v1", "kind": "DeleteOptions", "preconditions": {"uid": uid}}
        # The server checks the UID atomically; a name-only check races replacement.
        self._run(["delete", "--raw", path, "-f", "-"], stdin=json.dumps(options))
        self.deps.log(f"requested UID-preconditioned deletion of {resource}/{name}")

    def replace_owned(self, resource: str, name: str, uid: str, changes: dict) -> None:
        if resource != ESS_DATABASE or set(changes) != {"dataEviction"}:
            raise SuiteFailure("unsupported owned update")
        obj = self.get(resource, name)
        meta = (obj or {}).get("metadata") or {}
        if meta.get("uid") != uid or not meta.get("resourceVersion"):
            raise SuiteFailure("owned update identity or version is unverified")
        params = ((obj.get("spec") or {}).get("forProvider"))
        if not isinstance(params, dict):
            raise SuiteFailure("owned update spec is unavailable")
        params.update(changes)
        obj.pop("status", None)
        # The API enforces resourceVersion and immutable UID on replacement.
        self._run(["replace", "-f", "-"], stdin=json.dumps(obj))
        self.deps.log(f"updated owned {resource}/{name}")

    def delete(self, resource: str, name: str, namespace: str | None = None) -> None:
        args = ["delete", resource, name, "--ignore-not-found", "--wait=false"]
        if namespace:
            args.extend(["-n", namespace])
        self._run(args)
        self.deps.log(f"deleted {resource}/{name}")

    def get(self, resource: str, name: str, namespace: str | None = None):
        args = ["get", resource, name, "-o", "json"]
        if namespace:
            args.extend(["-n", namespace])
        proc = self._run(args, check=False)
        if proc.returncode != 0:
            err = proc.stderr.lower()
            if "not found" in err or "notfound" in err:
                return None
            raise SuiteFailure(redact(f"kubectl get {resource}/{name} failed", self.deps.secrets))
        return json.loads(proc.stdout)

    def record_controller_evidence(self, run_name: str) -> None:
        pods = json.loads(self._run(["get", "pods", "-A", "-o", "json"]).stdout).get("items") or []
        providers = [pod for pod in pods
                     if str((pod.get("metadata") or {}).get("name") or "").startswith("provider-rediscloud-")]
        if len(providers) != 1:
            raise SuiteFailure("provider runtime pod identity is missing or ambiguous")
        pod = providers[0]
        statuses = (pod.get("status") or {}).get("containerStatuses") or []
        runtime = next((s for s in statuses if s.get("name") == "package-runtime" and s.get("ready")), None)
        digest = re.search(r"sha256:[0-9a-f]{64}", str((runtime or {}).get("imageID") or ""))
        if not runtime or not digest:
            raise SuiteFailure("provider runtime digest is missing or runtime is not Ready")
        name = pod["metadata"]["name"]
        namespace = pod["metadata"]["namespace"]
        logs = self._run(["logs", name, "-n", namespace, "-c", "package-runtime", "--timestamps=true"]).stdout
        matches = sum(run_name in line for line in logs.splitlines())
        if not matches:
            raise SuiteFailure("controller logs do not identify this run's managed resources")
        # Print only a match count: controller messages may contain private IDs
        # and endpoints that are not known to the harness redactor.
        # The regex restricts this output to an image digest. Do not pass it
        # through substring ID redaction, which would corrupt the digest.
        print(f"provider runtime image {digest.group(0)}", flush=True)
        self.deps.log(f"controller log lines matched run={matches}")

    def require_deleted_event(self, uid: str, label: str) -> None:
        events = json.loads(self._run(["get", "events", "-A", "-o", "json"]).stdout).get("items") or []
        matching = [event for event in events
                    if (event.get("involvedObject") or {}).get("uid") == uid
                    and event.get("reason") == "DeletedExternalResource"
                    and event.get("type") == "Normal"]
        if not matching:
            raise SuiteFailure(f"{label} controller deletion event missing")
        self.deps.log(f"{label} controller event DeletedExternalResource observed")

    def secret_key_names(self, namespace: str, name: str) -> list[str]:
        secret = self.get("secret", name, namespace=namespace)
        if secret is None:
            raise SuiteFailure(f"connection secret {namespace}/{name} is missing")
        return secret_key_names(secret)

    def payment_method_id(self) -> str:
        return self._lookup_id("payment")

    def plan_id(self) -> str:
        return self._lookup_id("plan")

    def _lookup_id(self, suffix: str) -> str:
        kind = PAYMENT if suffix == "payment" else PLAN
        obj = self.get(kind, f"{PREFIX}{self.deps.run_id}-{suffix}")
        if not obj:
            return ""
        value = ((obj.get("status") or {}).get("atProvider") or {}).get("id") or ""
        return str(value)

    def _run(self, args: list[str], stdin: str | None = None, check: bool = True):
        cmd = [self.kubectl, "--context", self.context, *args]
        proc = subprocess.run(
            cmd,
            input=stdin,
            text=True,
            capture_output=True,
            check=False,
        )
        if check and proc.returncode != 0:
            message = redact(proc.stderr.strip() or f"kubectl exited {proc.returncode}", self.deps.secrets)
            raise SuiteFailure(message)
        return proc


def load_credentials(path: str | None, env_json: str | None) -> dict:
    if path:
        raw = json.loads(Path(path).read_text())
    elif env_json:
        raw = json.loads(env_json)
    else:
        raise SuiteFailure("credentials are required")
    if not isinstance(raw, dict):
        raise SuiteFailure("credentials must be a JSON object")
    return normalize_credentials(raw)


def build_deps(args, credentials: dict) -> Deps:
    assert_safe_context(args.context)
    secrets = [credentials["api_key"], credentials["secret_key"]]
    if credentials.get("url"):
        secrets.append(credentials["url"])
    cloud = RedisCloud(credentials.get("url") or DEFAULT_API, credentials["api_key"], credentials["secret_key"])
    deps = Deps(cloud, cluster=None, run_id=args.run_id, secrets=secrets)
    deps.cluster = KubectlCluster(args.kubectl, args.context, credentials, deps)
    return deps


def default_run_id() -> str:
    return os.environ.get("GITHUB_RUN_ID") or time.strftime("%Y%m%d%H%M%S")


def parse_args(argv: list[str] | None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Run the QA create-and-delete suite.")
    parser.add_argument("--credentials-file", default=os.environ.get("QA_CREDENTIALS_FILE"))
    parser.add_argument("--context", default=os.environ.get("QA_KUBE_CONTEXT", ALLOWED_CONTEXT))
    parser.add_argument("--kubectl", default=os.environ.get("QA_KUBECTL", "kubectl"))
    parser.add_argument("--run-id", default=default_run_id())
    parser.add_argument("--cleanup-only", action="store_true")
    parser.add_argument("--strict-essentials", action="store_true")
    parser.add_argument("--preflight-only", action="store_true")
    parser.add_argument("--task-diagnostic", action="store_true")
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    deps = None
    try:
        credentials = load_credentials(args.credentials_file, os.environ.get("UPTEST_CLOUD_CREDENTIALS"))
        if args.task_diagnostic:
            if args.cleanup_only or args.strict_essentials or args.preflight_only:
                raise SuiteFailure("task diagnostic cannot be combined with other modes")
            task_diagnostic(credentials)
            return 0
        if args.preflight_only:
            if args.cleanup_only or args.strict_essentials:
                raise SuiteFailure("preflight cannot be combined with mutating modes")
            preflight(credentials)
            return 0
        if args.strict_essentials:
            if args.cleanup_only:
                raise SuiteFailure("strict qualification cannot use shared-prefix cleanup")
            if not credentials.get("url") or not matches_expected_qa_origin(credentials["url"]):
                raise SuiteFailure("strict qualification requires the explicit approved QA endpoint")
            verify_checkpoint_artifact(args.run_id, credentials)
        deps = build_deps(args, credentials)
        if args.cleanup_only:
            cleanup_only(deps)
        elif args.strict_essentials:
            strict_essentials(deps)
        else:
            run(deps)
    except SuiteFailure as err:
        secrets = deps.secrets if deps is not None else []
        print(redact(str(err), secrets), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

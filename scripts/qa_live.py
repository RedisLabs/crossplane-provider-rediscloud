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
import json
import os
import secrets
import string
import subprocess
import sys
import time
import urllib.error
import urllib.request
from pathlib import Path

PREFIX = "xp-ci-"
REQUIRED_SECRET_KEYS = ("public_endpoint", "private_endpoint")
ALLOWED_CONTEXT = "kind-local-dev"
ACL_RULE = "+@read ~*"
DEFAULT_API = "https://api.redislabs.com/v1"
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
        subscription_code = delete_subscription(deps, state)
        problems = success_problems("active", "active", keys, database_code, subscription_code)
        if problems:
            raise SuiteFailure("; ".join(problems))
        run_essentials(deps, state, payment_id)
        deps.log("qa live ok")
    except Exception:
        cleanup(deps, state)
        raise
    finally:
        delete_lookups(deps)


def cleanup_only(deps) -> None:
    """Delete leftover xp-ci- objects. Do not create anything."""
    if not clear_or_refuse(deps):
        raise SuiteFailure("xp-ci- subscription still present")
    deps.log("cleanup ok")


def clear_or_refuse(deps) -> bool:
    if not prefixed_leftovers(deps):
        return True
    deps.log("deleting leftover xp-ci- objects")
    delete_prefixed(deps, deps.cloud.list_acl_users(), USER, deps.cloud.delete_acl_user)
    delete_prefixed(deps, deps.cloud.list_acl_roles(), ROLE, deps.cloud.delete_acl_role)
    delete_prefixed(deps, deps.cloud.list_acl_rules(), ACL, deps.cloud.delete_acl_rule)
    for sub in names_with_prefix(deps.cloud.list_subscriptions()):
        name = str(sub.get("name") or "")
        deps.cluster.delete(DATABASE, name)
        deps.cluster.delete(SUBSCRIPTION, name)
        sub_id = sub.get("id")
        if sub_id is not None:
            if not delete_leftover_databases(
                deps,
                str(sub_id),
                deps.cloud.list_databases,
                deps.cloud.delete_database,
            ):
                deps.log("xp-ci- subscription still present; refusing to create")
                return False
            deps.cloud.delete_subscription(str(sub_id))
    for sub in names_with_prefix(deps.cloud.list_fixed_subscriptions()):
        name = str(sub.get("name") or "")
        deps.cluster.delete(ESS_DATABASE, name)
        deps.cluster.delete(ESS_SUBSCRIPTION, name)
        sub_id = sub.get("id")
        if sub_id is not None:
            if not delete_leftover_databases(
                deps,
                str(sub_id),
                deps.cloud.list_fixed_databases,
                deps.cloud.delete_fixed_database,
            ):
                deps.log("xp-ci- subscription still present; refusing to create")
                return False
            deps.cloud.delete_fixed_subscription(str(sub_id))
    if not wait_until(
        deps,
        "leftover xp-ci- objects to disappear",
        deps.timeouts["delete"],
        lambda: not prefixed_leftovers(deps),
    ):
        deps.log("xp-ci- subscription still present; refusing to create")
        return False
    return True


def delete_leftover_databases(deps, sub_id: str, list_dbs, delete_db) -> bool:
    _code, databases = list_dbs(sub_id)
    if not databases:
        return True
    for row in databases:
        db_id = row.get("databaseId")
        if db_id is None:
            db_id = row.get("id")
        if db_id is not None:
            delete_db(sub_id, str(db_id))
    return wait_until(
        deps,
        "leftover xp-ci- databases to disappear",
        deps.timeouts["delete"],
        lambda: not list_dbs(sub_id)[1],
    )


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


def delete_prefixed(deps, rows: list, resource: str, cloud_delete) -> None:
    for row in names_with_prefix(rows):
        name = str(row.get("name") or "")
        deps.cluster.delete(resource, name)
        row_id = row.get("id")
        if row_id is not None:
            cloud_delete(str(row_id))


def create_acl(deps, state: State) -> None:
    state.acl_name = state.run_name
    deps.cluster.apply(acl_manifest(state.run_name))
    if not wait_until(
        deps,
        f"ACL {state.run_name}",
        deps.timeouts["acl"],
        lambda: acl_is_active(deps, state.run_name),
    ):
        raise SuiteFailure(f"ACL {state.run_name} did not become active")


def delete_acl(deps, state: State) -> None:
    deps.cluster.delete(ACL, state.run_name)
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
    deps.cluster.apply(subscription_manifest(state.run_name, payment_id, cidr))
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
    deps.cluster.apply(database_manifest(state.run_name, state.subscription_id))
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
    deps.cluster.delete(DATABASE, state.database_name)
    code = wait_http_404(
        deps,
        f"database {state.database_name}",
        lambda: deps.cloud.get_database(state.subscription_id, state.database_id),
    )
    state.database_deleted = code == 404
    return code


def delete_subscription(deps, state: State) -> int:
    deps.log(f"deleting subscription {state.subscription_name}")
    deps.cluster.delete(SUBSCRIPTION, state.subscription_name)
    code = wait_http_404(
        deps,
        f"subscription {state.subscription_name}",
        lambda: deps.cloud.get_subscription(state.subscription_id),
    )
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
    deps.cluster.apply(role_manifest(state.run_name, state.run_name, state.subscription_id, state.database_id))
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
    deps.cluster.apply(password_secret_manifest(state.password_secret, password))
    state.user_name = state.run_name
    deps.cluster.apply(user_manifest(state.run_name, state.run_name, state.password_secret))
    if not wait_until(
        deps,
        f"ACL user {state.run_name}",
        deps.timeouts["acl"],
        lambda: named_is_active(deps, USER, state.run_name, deps.cloud.list_acl_users()),
    ):
        raise SuiteFailure(f"ACL user {state.run_name} did not become active")
    deps.cluster.delete(USER, state.run_name)
    if not wait_until(
        deps,
        f"ACL user {state.run_name} delete",
        deps.timeouts["delete"],
        lambda: find_named(deps.cloud.list_acl_users(), state.run_name) is None,
    ):
        raise SuiteFailure(f"ACL user {state.run_name} is still present")
    deps.cluster.delete("secret", state.password_secret, namespace="default")
    state.user_name = None
    deps.cluster.delete(ROLE, state.run_name)
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


def run_essentials(deps, state: State, payment_id: str) -> None:
    existing = deps.cloud.list_fixed_subscriptions()
    if existing:
        raise SuiteFailure("account already has an essentials subscription")
    plan_id = str(deps.cluster.plan_id() or "").strip()
    if not plan_id:
        raise SuiteFailure("essentials plan lookup has no id")
    deps.remember_secret(plan_id)
    state.essentials_name = state.run_name
    deps.cluster.apply(essentials_subscription_manifest(state.run_name, plan_id, payment_id))
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
    deps.cluster.apply(essentials_database_manifest(state.run_name, state.essentials_id))
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
    deps.cluster.delete(ESS_DATABASE, state.run_name)
    database_code = wait_http_404(
        deps,
        f"essentials database {state.run_name}",
        lambda: deps.cloud.get_fixed_database(state.essentials_id, state.essentials_database_id),
    )
    state.essentials_database_deleted = database_code == 404
    deps.cluster.delete(ESS_SUBSCRIPTION, state.run_name)
    subscription_code = wait_http_404(
        deps,
        f"essentials {state.run_name}",
        lambda: deps.cloud.get_fixed_subscription(state.essentials_id),
    )
    state.essentials_deleted = subscription_code == 404
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


def cleanup(deps, state: State) -> None:
    if state.user_name:
        deps.cluster.delete(USER, state.user_name)
        state.user_name = None
    if state.password_secret:
        deps.cluster.delete("secret", state.password_secret, namespace="default")
        state.password_secret = None
    if state.role_name:
        deps.cluster.delete(ROLE, state.role_name)
        state.role_name = None
    if state.essentials_database_name and not state.essentials_database_deleted:
        deps.cluster.delete(ESS_DATABASE, state.essentials_database_name)
        state.essentials_database_deleted = True
    if state.essentials_name and not state.essentials_deleted:
        deps.cluster.delete(ESS_SUBSCRIPTION, state.essentials_name)
        state.essentials_deleted = True
    if state.database_name and not state.database_deleted:
        deps.log(f"cleanup delete database {state.database_name}")
        deps.cluster.delete(DATABASE, state.database_name)
        state.database_deleted = True
    if state.subscription_name and not state.subscription_deleted:
        deps.log(f"cleanup delete subscription {state.subscription_name}")
        deps.cluster.delete(SUBSCRIPTION, state.subscription_name)
        state.subscription_deleted = True
    if state.acl_name:
        deps.log(f"cleanup delete ACL {state.acl_name}")
        deps.cluster.delete(ACL, state.acl_name)
        state.acl_name = None


def wait_until(deps, label: str, timeout: float, ready) -> bool:
    deadline = deps.now() + timeout
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
    for manifest in lookup_manifests(run_id):
        deps.cluster.apply(manifest)
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
    for resource, suffix in (
        (PAYMENT, "payment"),
        (PLAN, "plan"),
        (REGIONS, "regions"),
    ):
        deps.cluster.delete(resource, f"{PREFIX}{run_id}-{suffix}")
    deps.cluster.delete("secret", "provider-secret", namespace="upbound-system")
    deps.cluster.delete("providerconfig.rediscloud.redis.io", "default")


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
        self.timeouts = {
            "lookup": 300.0,
            "acl": 300.0,
            "subscription": 1800.0,
            "database": 1200.0,
            "delete": 900.0,
        }
        self.lookups_applied = False
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
            with urllib.request.urlopen(req, timeout=60) as resp:
                raw = resp.read()
                body = json.loads(raw.decode()) if raw else {}
                return resp.status, body
        except urllib.error.HTTPError as err:
            err.close()
            return err.code, None

    def list_subscriptions(self) -> list:
        code, body = self.request("GET", "/subscriptions")
        if code != 200:
            raise SuiteFailure(f"list subscriptions HTTP {code}")
        return subscription_rows(body)

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

    def get_database(self, sub_id: str, db_id: str):
        return self.request("GET", f"/subscriptions/{sub_id}/databases/{db_id}")

    def delete_database(self, sub_id: str, db_id: str) -> None:
        self._delete(f"/subscriptions/{sub_id}/databases/{db_id}", "delete database")

    def list_acl_rules(self) -> list:
        return self._rows("GET", "/acl/redisRules", "list ACL rules", acl_rows)

    def delete_acl_rule(self, rule_id: str) -> None:
        self._delete(f"/acl/redisRules/{rule_id}", "delete ACL rule")

    def list_acl_roles(self) -> list:
        return self._rows("GET", "/acl/roles", "list ACL roles", lambda body: named_rows(body, "roles"))

    def delete_acl_role(self, role_id: str) -> None:
        self._delete(f"/acl/roles/{role_id}", "delete ACL role")

    def list_acl_users(self) -> list:
        return self._rows("GET", "/acl/users", "list ACL users", lambda body: named_rows(body, "users"))

    def delete_acl_user(self, user_id: str) -> None:
        self._delete(f"/acl/users/{user_id}", "delete ACL user")

    def list_fixed_subscriptions(self) -> list:
        return self._rows(
            "GET",
            "/fixed/subscriptions",
            "list essentials subscriptions",
            subscription_rows,
        )

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
        for manifest in provider_config_manifests(payload):
            self.apply(manifest)
        self.deps.log("provider config installed")

    def apply(self, manifest: dict) -> None:
        self._run(["apply", "-f", "-"], stdin=json.dumps(manifest))
        name = manifest.get("metadata", {}).get("name", "")
        self.deps.log(f"applied {manifest.get('kind')} {name}")

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
    return parser.parse_args(argv)


def main(argv: list[str] | None = None) -> int:
    args = parse_args(argv)
    deps = None
    try:
        credentials = load_credentials(args.credentials_file, os.environ.get("UPTEST_CLOUD_CREDENTIALS"))
        deps = build_deps(args, credentials)
        if args.cleanup_only:
            cleanup_only(deps)
        else:
            run(deps)
    except SuiteFailure as err:
        secrets = deps.secrets if deps is not None else []
        print(redact(str(err), secrets), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())

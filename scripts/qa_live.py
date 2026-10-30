#!/usr/bin/env python3
"""Create and delete one QA ACL rule, one Pro subscription, and one database.

The workflow that calls this script triggers on schedule and workflow_dispatch
only. It does not run on pull requests. Logs print status codes, Ready
reasons, and secret key names. They do not print secret values, API keys, or
the payment method id.
"""

from __future__ import annotations

import argparse
import json
import os
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
ACL = "rule.acl.redis.io"
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
    if isinstance(body, dict):
        return list(body.get("redisRules") or [])
    if isinstance(body, list):
        return body
    return []


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


class State:
    def __init__(self, run_name: str):
        self.run_name = run_name
        self.subscription_name = None
        self.database_name = None
        self.acl_name = None
        self.subscription_id = None
        self.database_id = None
        self.subscription_deleted = False
        self.database_deleted = False
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
        run_acl(deps, state)
        run_subscription(deps, state, payment_id)
        run_database(deps, state)
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
    leftovers = names_with_prefix(deps.cloud.list_subscriptions())
    if not leftovers:
        return True
    deps.log(f"deleting {len(leftovers)} leftover xp-ci- subscription(s)")
    for sub in leftovers:
        name = str(sub.get("name") or "")
        deps.cluster.delete(DATABASE, name)
        deps.cluster.delete(SUBSCRIPTION, name)
        sub_id = sub.get("id")
        if sub_id is not None:
            deps.cloud.delete_subscription(str(sub_id))
    if not wait_until(
        deps,
        "leftover xp-ci- subscriptions to disappear",
        deps.timeouts["delete"],
        lambda: not names_with_prefix(deps.cloud.list_subscriptions()),
    ):
        deps.log("xp-ci- subscription still present; refusing to create")
        return False
    return True


def run_acl(deps, state: State) -> None:
    state.acl_name = state.run_name
    deps.cluster.apply(acl_manifest(state.run_name))
    if not wait_until(
        deps,
        f"ACL {state.run_name}",
        deps.timeouts["acl"],
        lambda: acl_is_active(deps, state.run_name),
    ):
        raise SuiteFailure(f"ACL {state.run_name} did not become active")
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
        holder["id"] = cr_id
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


def cleanup(deps, state: State) -> None:
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

    def list_acl_rules(self) -> list:
        code, body = self.request("GET", "/acl/redisRules")
        if code != 200:
            raise SuiteFailure(f"list ACL rules HTTP {code}")
        return acl_rows(body)


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
        obj = self.get(PAYMENT, f"{PREFIX}{self.deps.run_id}-payment")
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

"""Credential-free RBAC probes in an isolated Actions Kind cluster only."""

import json
import os
import subprocess
import sys

if os.environ.get("GITHUB_ACTIONS") != "true":
    raise SystemExit("hosted Actions required")

KUBECTL = ".cache/tools/linux_x86_64/kubectl-v1.33.5"


def run(args, payload=None):
    return subprocess.run(
        [KUBECTL, "--request-timeout=15s", *args],
        input=json.dumps(payload) if payload is not None else None,
        capture_output=True,
        text=True,
        timeout=25,
        check=False,
    )


def read_json(args):
    result = run(args)
    if result.returncode != 0:
        raise SystemExit("Kubernetes inventory failed")
    return json.loads(result.stdout)


pods = read_json(["-n", "upbound-system", "get", "pods", "-o", "json"])["items"]
providers = [
    pod for pod in pods
    if pod["metadata"]["name"].startswith("provider-rediscloud-")
    and not pod["metadata"].get("deletionTimestamp")
    and any(c["name"] == "package-runtime" and c.get("ready") for c in pod.get("status", {}).get("containerStatuses", []))
]
if len(providers) != 1 or providers[0]["spec"].get("hostNetwork"):
    raise SystemExit("expected one ready non-host-network provider")
provider = providers[0]
account = provider["spec"]["serviceAccountName"]
identity = f"system:serviceaccount:upbound-system:{account}"
namespace_items = read_json(["get", "namespaces", "-o", "json"])["items"]
namespaces = sorted(item["metadata"]["name"] for item in namespace_items)
if not namespaces or len(namespaces) != len(set(namespaces)):
    raise SystemExit("namespace inventory is empty or duplicated")


def allowed(verb, group, resource, subresource="", namespace="", name=""):
    attributes = {"verb": verb, "group": group, "resource": resource}
    if subresource:
        attributes["subresource"] = subresource
    if namespace:
        attributes["namespace"] = namespace
    if name:
        attributes["name"] = name
    request = {"apiVersion": "authorization.k8s.io/v1", "kind": "SelfSubjectAccessReview", "spec": {"resourceAttributes": attributes}}
    result = run([f"--as={identity}", "create", "-f", "-", "-o", "json"], request)
    if result.returncode != 0:
        raise SystemExit("authorization review request failed")
    status = json.loads(result.stdout).get("status", {})
    if type(status.get("allowed")) is not bool or status.get("evaluationError"):
        raise SystemExit("authorization review was incomplete")
    return status["allowed"]


if not allowed("get", "lookup.redis.io", "regions", name="boundary-gcp"):
    raise SystemExit("positive controller authorization control failed")

# Test all currently present namespaces; this is a finite set of relay and
# security-policy controls, not a proof that every API operation is harmless.
namespaced = [
    ("create", "", "pods", ""),
    ("update", "", "pods", "ephemeralcontainers"),
    ("create", "", "pods", "exec"),
    ("create", "", "pods", "attach"),
    ("create", "", "pods", "portforward"),
    ("get", "", "pods", "proxy"),
    ("get", "", "services", "proxy"),
    ("create", "", "services", ""),
    ("create", "", "endpoints", ""),
    ("create", "discovery.k8s.io", "endpointslices", ""),
    ("create", "apps", "deployments", ""),
    ("create", "batch", "jobs", ""),
    ("create", "networking.k8s.io", "networkpolicies", ""),
    ("update", "networking.k8s.io", "networkpolicies", ""),
    ("create", "", "serviceaccounts", "token"),
    ("create", "rbac.authorization.k8s.io", "roles", ""),
    ("create", "rbac.authorization.k8s.io", "rolebindings", ""),
]
for namespace in namespaces:
    for verb, group, resource, subresource in namespaced:
        if allowed(verb, group, resource, subresource, namespace):
            raise SystemExit(f"forbidden relay/policy authorization is allowed: {verb} {resource}/{subresource}")

cluster_scoped = [
    ("create", "rbac.authorization.k8s.io", "clusterroles", ""),
    ("create", "rbac.authorization.k8s.io", "clusterrolebindings", ""),
    ("impersonate", "", "users", ""),
    ("impersonate", "", "groups", ""),
    ("impersonate", "", "serviceaccounts", ""),
]
for verb, group, resource, subresource in cluster_scoped:
    if allowed(verb, group, resource, subresource):
        raise SystemExit(f"forbidden cluster authorization is allowed: {verb} {resource}/{subresource}")

# Exercise real authorization paths as the provider identity. The direct pod
# creation uses Kubernetes server-side dry-run, so it cannot create a pod.
name = provider["metadata"]["name"]
paths = [
    f"/api/v1/namespaces/upbound-system/pods/{name}/proxy/",
    "/api/v1/namespaces/upbound-system/services/boundary-fixture/proxy/",
]
for path in paths:
    result = run([f"--as={identity}", "get", "--raw", path])
    if result.returncode == 0 or "forbidden" not in result.stderr.lower():
        raise SystemExit("direct proxy denial could not be verified")

pod = {"apiVersion": "v1", "kind": "Pod", "metadata": {"name": "forbidden-fixture", "namespace": "upbound-system"}, "spec": {"containers": [{"name": "fixture", "image": "registry.invalid/fixture"}]}}
result = run([f"--as={identity}", "create", "--dry-run=server", "-f", "-"], pod)
if result.returncode == 0 or "forbidden" not in result.stderr.lower():
    raise SystemExit("direct dry-run pod creation denial could not be verified")

current = sorted(item["metadata"]["name"] for item in read_json(["get", "namespaces", "-o", "json"])["items"])
if current != namespaces:
    raise SystemExit("namespace inventory changed during authorization checks")
print(f"Provider identity: positive read plus {len(namespaces) * len(namespaced) + len(cluster_scoped)} sampled relay/policy reviews and three direct denial checks passed.")
print("This finite RBAC sample is fake-cluster evidence, not complete API confinement or live QA authorization.")

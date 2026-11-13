"""GCP verification adapter; invoke from an exact-baseline Actions checkout.

Keep this harness separate from the provider source being built. Evidence output
contains only selected fields. The caller must run cleanup with if: always().
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
from datetime import datetime, timezone

BASELINE = '5ceb4431079b46fe79159b9ff637d4a3f1efb0e6'
QA_URL = 'https://capi.k8s-rcp.sm-qa.qa.redislabs.com/v1'



def safe_status(obj):
    """Exclude messages, spec, connection details, and arbitrary provider fields."""
    if obj is None:
        return {"absent": True}
    meta, status = obj.get("metadata", {}), obj.get("status", {})
    return {"kind": obj.get("kind"), "name": meta.get("name"),
            "created": meta.get("creationTimestamp"),
            "external_id": meta.get("annotations", {}).get("crossplane.io/external-name"),
            "id": status.get("atProvider", {}).get("id"),
            "conditions": [{k: row[k] for k in
                            ("type", "status", "reason", "lastTransitionTime") if k in row}
                           for row in status.get("conditions", [])]}


def remember_id(q, state, kind, name, obj, write):
    if name != state.run_name or obj is None:
        return
    if obj.get("metadata", {}).get("name") != name:
        raise RuntimeError("Resource ownership mismatch")
    row = safe_status(obj)
    raw = row["id"] or row["external_id"]
    if raw is None:
        return
    parts = str(raw).split("/")
    if not all(part.isdigit() for part in parts):
        return
    if kind == q.SUBSCRIPTION and len(parts) == 1:
        if state.subscription_id and state.subscription_id != parts[0]:
            state.id_conflict = True
            write("state.json", vars(state))
            raise RuntimeError("Subscription ID changed")
        state.subscription_id = parts[0]
    elif kind == q.DATABASE:
        if len(parts) == 2:
            if state.subscription_id and state.subscription_id != parts[0]:
                raise RuntimeError("Database subscription mismatch")
            state.subscription_id, state.database_id = parts
        elif len(parts) == 1:
            state.database_id = parts[0]
    write("state.json", vars(state))


def discover_subscription(state, name, rows, write):
    matches = [row for row in rows if row.get("name") == name]
    ids = [str(row["id"]) for row in matches if str(row.get("id", "")).isdigit()]
    if len(matches) > 1 or (ids and state.subscription_id and
                            state.subscription_id != ids[0]):
        state.id_conflict = True
        write("state.json", vars(state))
        raise RuntimeError("Ambiguous subscription ownership")
    if ids:
        state.subscription_id = ids[0]
        write("state.json", vars(state))


def capture_failure(q, cluster, name, write):
    rows = []
    for kind, resource_name in (
        ("provider.pkg.crossplane.io", "provider-rediscloud"),
        ("providerconfig.rediscloud.redis.io", "default"),
        (q.SUBSCRIPTION, name), (q.DATABASE, name), (q.ACL, name),
    ):
        try:
            rows.append({"resource": kind, "name": resource_name,
                         "status": safe_status(cluster.get(kind, resource_name))})
        except Exception:
            rows.append({"resource": kind, "name": resource_name,
                         "read_failed": True})
    write("failure-kubernetes.json", rows)


def cleanup_resources(q, deps, cluster, api, name, state, write):
    if state.get("run_name") != name:
        raise RuntimeError("Cleanup state belongs to another run")
    proof = {"run": name, "resources": [], "crs_absent": False}
    child_absent = False
    failed = bool(state.get("id_conflict"))
    for kind in (q.DATABASE, q.SUBSCRIPTION, q.ACL):
        result = {"resource": kind, "name": name, "absent": False}
        if kind == q.SUBSCRIPTION and not child_absent:
            result["skipped"] = "dependent database not confirmed absent"
        else:
            try:
                cluster.delete(kind, name)
                result["absent"] = q.wait_until(
                    deps, "finalizer completion", 900,
                    lambda: cluster.get(kind, name) is None)
            except Exception:
                result["operation_failed"] = True
        if kind == q.DATABASE:
            child_absent = result["absent"]
        failed |= not result["absent"]
        proof["resources"].append(result)
        write("06-cleanup.json", proof)
    proof["crs_absent"] = all(r["absent"] for r in proof["resources"])
    try:
        proof["remaining_prefix"] = [r["name"] for rows in
            (api.list_subscriptions(), api.list_acl_rules()) for r in rows
            if str(r.get("name", "")).startswith(name)]
        failed |= bool(proof["remaining_prefix"])
        sub_id, db_id = state.get("subscription_id"), state.get("database_id")
        if sub_id:
            proof["subscription_http"] = api.get_subscription(sub_id)[0]
            failed |= proof["subscription_http"] != 404
            if db_id:
                proof["database_http"] = api.get_database(sub_id, db_id)[0]
                failed |= proof["database_http"] != 404
        else:
            proof["id_verification"] = "unavailable"
            failed = True
    except Exception:
        proof["cloud_verification_failed"] = True
        failed = True
    proof["result"] = "FAILED" if failed else "PASSED"
    write("06-cleanup.json", proof)
    if failed:
        raise RuntimeError("Cleanup incomplete; retain credentials")
    deps.lookups_applied = True
    try:
        q.delete_lookups(deps)
    except Exception:
        proof["result"] = "FAILED"
        proof["lookup_cleanup_failed"] = True
        write("06-cleanup.json", proof)
        raise RuntimeError("Lookup cleanup incomplete") from None


def main():
    p = argparse.ArgumentParser()
    p.add_argument('phase', choices=['create', 'cleanup'])
    p.add_argument('--baseline', required=True)
    p.add_argument('--kubectl', required=True)
    p.add_argument('--output', required=True)
    a = p.parse_args()
    if os.environ.get('GITHUB_ACTIONS') != 'true':
        raise RuntimeError('This harness runs only in GitHub Actions')
    repo = Path(a.baseline).resolve()
    if subprocess.check_output(['git', '-C', str(repo), 'rev-parse', 'HEAD'], text=True).strip() != BASELINE:
        raise RuntimeError('Provider checkout is not the tested baseline')
    sys.path.insert(0, str(repo / 'scripts'))
    import qa_live as q
    c = q.normalize_credentials(json.loads(os.environ['UPTEST_CLOUD_CREDENTIALS']))
    if c.get('url') != QA_URL:
        raise RuntimeError('Explicit QA endpoint required; production fallback prohibited')
    run_id = 'gcp-' + os.environ['GITHUB_RUN_ID'] + '-' + os.environ['GITHUB_RUN_ATTEMPT']
    q.PREFIX = 'xp-red225344-'
    name = q.PREFIX + run_id
    out = Path(a.output)
    out.mkdir(parents=True, exist_ok=True)
    api = q.RedisCloud(c['url'], c['api_key'], c['secret_key'])
    deps = q.Deps(api, None, run_id, [c['api_key'], c['secret_key']])
    cluster = q.KubectlCluster(a.kubectl, q.ALLOWED_CONTEXT, c, deps)
    deps.cluster = cluster
    def write(filename, data):
        (out / filename).write_text(json.dumps(data, indent=2) + '\n')
    def get_status(kind, resource_name):
        obj = cluster.get(kind, resource_name)
        if obj is None:
            raise RuntimeError('Expected CR absent')
        return safe_status(obj)
    if a.phase == 'create':
        for rows in (api.list_subscriptions(), api.list_acl_rules()):
            if any(str(r.get('name', '')).startswith(q.PREFIX) for r in rows):
                raise RuntimeError('Existing verification resources; refusing new run')
        state = q.State(name)
        write('state.json', vars(state))
        original_get = cluster.get
        def observed_get(kind, resource_name):
            obj = original_get(kind, resource_name)
            remember_id(q, state, kind, resource_name, obj, write)
            return obj
        cluster.get = observed_get
        original_list = api.list_subscriptions
        def observed_subscriptions():
            rows = original_list()
            discover_subscription(state, name, rows, write)
            return rows
        api.list_subscriptions = observed_subscriptions

        try:
            cluster.install_provider_config()
            q.apply_lookups(deps)
            payment = str(cluster.payment_method_id() or '')
            if not payment:
                raise RuntimeError('Payment lookup failed')
            deps.remember_secret(payment)
            original = q.subscription_manifest
            def manifest(*args):
                result = original(*args)
                cp = result['spec']['forProvider']['cloudProvider'][0]
                cp['provider'] = 'GCP'
                cp['region'][0]['region'] = 'us-central1'
                return result
            q.subscription_manifest = manifest
            apply = cluster.apply
            def recorded_apply(obj):
                if obj['kind'] != 'Secret':
                    with (out / '01-trigger.txt').open('a') as f:
                        f.write(f"kubectl --context {q.ALLOWED_CONTEXT} apply -f - # {obj['kind']}/{obj['metadata']['name']}\n")
                apply(obj)
            cluster.apply = recorded_apply
            (out / '01-trigger.txt').write_text(f'Crossplane; Kind hosts Crossplane and CRDs\nProvider commit: {BASELINE}\nRun: {name}\n')
            for action in (lambda:q.create_acl(deps,state), lambda:q.run_subscription(deps,state,payment), lambda:q.run_database(deps,state)):
                action()
                write('state.json', vars(state))
            status = [get_status(k,n) for k,n in [('provider.pkg.crossplane.io','provider-rediscloud'),('providerconfig.rediscloud.redis.io','default'),(q.SUBSCRIPTION,name),(q.DATABASE,name),(q.ACL,name)]]
            write('02-kubernetes.json', status)
            for row in status[2:]:
                conditions = {x['type']:x['status'] for x in row['conditions']}
                if conditions.get('Ready') != 'True' or conditions.get('Synced') != 'True':
                    raise RuntimeError('Managed CR is not Ready and Synced')
            sc, sub = api.get_subscription(state.subscription_id)
            dc, db = api.get_database(state.subscription_id, state.database_id)
            rule = q.find_named(api.list_acl_rules(), name)
            if sc != 200 or dc != 200 or any(x.get('status') != 'active' for x in (sub, db, rule or {})):
                raise RuntimeError('Cloud resources not active')
            if str(sub['id']) != str(status[2]['id']) or str(status[2]['external_id']) != str(sub['id']):
                raise RuntimeError('Subscription ID mismatch')
            expected_db = str(sub['id']) + '/' + str(db['databaseId'])
            if str(status[3]['id']) != expected_db or str(status[3]['external_id']) != expected_db:
                raise RuntimeError('Database ID mismatch')
            if str(status[4]['id']) != str(rule['id']) or str(status[4]['external_id']) != str(rule['id']):
                raise RuntimeError('ACL ID mismatch')
            cloud_details = [{'provider':x.get('provider') or x.get('cloudProvider'),
                              'regions':[r.get('region') for r in x.get('regions',[])]}
                             for x in sub.get('cloudDetails',[])]
            if not any(x['provider'] == 'GCP' and 'us-central1' in x['regions'] for x in cloud_details):
                raise RuntimeError('Cloud region/provider mismatch')
            write('03-capi.json', {'run':name,'captured':datetime.now(timezone.utc).isoformat(),
                'subscription':{k:sub.get(k) for k in ('id','name','status','numberOfDatabases')},
                'cloudDetails':cloud_details,
                'database':{k:db.get(k) for k in ('databaseId','name','status','memoryLimitInGb','replication')},
                'acl':{k:rule.get(k) for k in ('id','name','status')},'ids_match':True})
        except Exception:
            # A create request may fail before the first CR polling iteration.
            try:
                observed_subscriptions()
            except Exception:
                write("failure-discovery.json", {"subscription_lookup_failed": True})
            capture_failure(q, cluster, name, write)
            raise
        finally:
            write('state.json', vars(state))
    else:
        if not (out / 'state.json').exists():
            raise RuntimeError('Cleanup state missing; absence unverified')
        state = json.loads((out / 'state.json').read_text())
        cleanup_resources(q, deps, cluster, api, name, state, write)


if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        # Raw API/controller exceptions may contain sensitive response fields.
        print('Verification failed: ' + type(exc).__name__, file=sys.stderr)
        sys.exit(1)

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
        m, s = obj['metadata'], obj.get('status', {})
        return {'kind':obj['kind'], 'name':m['name'], 'created':m['creationTimestamp'],
                'external_id':m.get('annotations', {}).get('crossplane.io/external-name'),
                'id':s.get('atProvider', {}).get('id'),
                'conditions':[{k:v for k,v in x.items() if k in ('type','status','reason','lastTransitionTime')} for x in s.get('conditions', [])]}
    if a.phase == 'create':
        for rows in (api.list_subscriptions(), api.list_acl_rules()):
            if any(str(r.get('name', '')).startswith(q.PREFIX) for r in rows):
                raise RuntimeError('Existing verification resources; refusing new run')
        state = q.State(name)
        write('state.json', vars(state))
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
        finally:
            write('state.json', vars(state))
    else:
        if not (out / 'state.json').exists():
            return
        state = json.loads((out / 'state.json').read_text())
        # These deterministic names also cover failure before ID discovery.
        for kind in (q.DATABASE, q.SUBSCRIPTION, q.ACL):
            cluster.delete(kind,name)
            if not q.wait_until(deps,'finalizer completion',900,lambda:cluster.get(kind,name) is None):
                raise RuntimeError('Cleanup finalizers incomplete; retain credentials')
        leftovers = [r['name'] for rows in (api.list_subscriptions(), api.list_acl_rules()) for r in rows if str(r.get('name','')).startswith(q.PREFIX)]
        proof = {'run':name,'crs_absent':True,'remaining_prefix':leftovers}
        if state.get('subscription_id'):
            proof['subscription_http'] = api.get_subscription(state['subscription_id'])[0]
            if state.get('database_id'):
                proof['database_http'] = api.get_database(state['subscription_id'],state['database_id'])[0]
        write('06-cleanup.json',proof)
        if leftovers or any(proof[k] != 404 for k in ('subscription_http','database_http') if k in proof):
            raise RuntimeError('Cloud cleanup verification failed')
        deps.lookups_applied = True
        q.delete_lookups(deps)


if __name__ == '__main__':
    try:
        main()
    except Exception as exc:
        # Raw API/controller exceptions may contain sensitive response fields.
        print('Verification failed: ' + type(exc).__name__, file=sys.stderr)
        sys.exit(1)

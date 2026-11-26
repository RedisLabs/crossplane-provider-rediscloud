#!/usr/bin/env bash
# Disposable GitHub Actions Kind fixture; fake Cloud only.
set -euo pipefail
[[ "${GITHUB_ACTIONS:-}" == true ]] || { echo 'hosted Actions required' >&2; exit 1; }
kubectl=.cache/tools/linux_x86_64/kubectl-v1.33.5
node=local-dev-control-plane
image='registry:2.8.3@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373'
original=$(cat "$RUNNER_TEMP/provider-pod.txt")
digest=$(cat "$RUNNER_TEMP/installed-digest")
"$kubectl" get node "$node" -o json > "$RUNNER_TEMP/hold-node.json"
python3 - "$RUNNER_TEMP/hold-node.json" "$RUNNER_TEMP/hold-node.env" <<'PY'
import ipaddress,json,pathlib,sys
n=json.loads(pathlib.Path(sys.argv[1]).read_text())
cidrs=[ipaddress.ip_network(x) for x in n['spec']['podCIDRs']]
ips=[ipaddress.ip_address(x['address']) for x in n['status']['addresses'] if x['type']=='InternalIP']
if len(cidrs)!=1 or cidrs[0].version!=4 or len(ips)!=1 or ips[0].version!=4:
    raise SystemExit('expected one IPv4-only disposable node and CIDR')
pathlib.Path(sys.argv[2]).write_text(f'node_ip={ips[0]}\ncidr={cidrs[0]}\n')
PY
source "$RUNNER_TEMP/hold-node.env"
cleanup_source=''
cleanup_node_source=''
cleanup_node_allow=''
cleanup_external_allow=''
cleanup(){
 if [[ -n "$cleanup_external_allow" && -n "${external_ip:-}" ]]; then docker exec "$node" iptables -D CODEX-HOLD -s "$cleanup_external_allow/32" -d "$external_ip/32" -p tcp --dport 5555 -j ACCEPT 2>/dev/null || true; fi
 if [[ -n "$cleanup_node_source" ]]; then docker exec "$node" iptables -D INPUT -s "$cleanup_node_source/32" -d "$node_ip/32" -p tcp --dport 5555 -m comment --comment codex-hold-node-flow 2>/dev/null || true; fi
 if [[ -n "$cleanup_node_allow" ]]; then docker exec "$node" iptables -D CODEX-HOLD -s "$cleanup_node_allow/32" -d "$node_ip/32" -p tcp --dport 5555 -j ACCEPT 2>/dev/null || true; fi
 if [[ -n "$cleanup_source" && -n "${external_ip:-}" ]]; then docker exec "$node" iptables -D FORWARD -s "$cleanup_source/32" -d "$external_ip/32" -p tcp --dport 5555 -m comment --comment codex-hold-flow 2>/dev/null || true; fi
 for chain in INPUT FORWARD; do docker exec "$node" iptables -D "$chain" -j CODEX-HOLD 2>/dev/null || true; done
 docker exec "$node" iptables -F CODEX-HOLD 2>/dev/null || true
 docker exec "$node" iptables -X CODEX-HOLD 2>/dev/null || true
 docker rm -f hold-fake hold-denied hold-external hold-registry >/dev/null 2>&1 || true
}
trap cleanup EXIT
CGO_ENABLED=0 go build -o "$RUNNER_TEMP/hold-fake" ./fixture-src/scripts/qa_kind_hold/fake_cloud.go
docker run -d --name hold-fake --network "container:$node" -v "$RUNNER_TEMP/hold-fake:/fixture:ro" --entrypoint /fixture "$image" >/dev/null
docker run -d --name hold-denied --network "container:$node" -e REGISTRY_HTTP_ADDR=0.0.0.0:5555 "$image" >/dev/null
docker run -d --name hold-external --network kind -e REGISTRY_HTTP_ADDR=0.0.0.0:5555 "$image" >/dev/null
external_ip=$(docker inspect hold-external --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}')
python3 - "$external_ip" "$node_ip" "$cidr" <<'PYIP'
import ipaddress,sys
x=ipaddress.ip_address(sys.argv[1]); assert x.version==4 and str(x)!=sys.argv[2] and x not in ipaddress.ip_network(sys.argv[3])
PYIP
node_wget(){ docker run --rm --network "container:$node" --entrypoint wget "$image" "$@"; }
for attempt in $(seq 1 60); do
 if node_wget -q -T 2 -O /dev/null --header='X-Fixture-Control: fixture-control' http://127.0.0.1:5556/fixture/state && node_wget -q -T 2 -O /dev/null http://127.0.0.1:5555/v2/; then break; fi
 sleep 1
done
node_wget -q -T 2 -O /dev/null --header='X-Fixture-Control: fixture-control' http://127.0.0.1:5556/fixture/state
node_wget -q -T 2 -O /dev/null http://127.0.0.1:5555/v2/
node_wget -q -T 2 -O /dev/null "http://$external_ip:5555/v2/"
# Kubelet pulls the replacement from a node-local copy of the same ephemeral
# registry; pod-CIDR deny otherwise blocks the registry Pod's return traffic.
"$kubectl" -n upbound-system get pods -l app=crossplane -o json > "$RUNNER_TEMP/hold-crossplane-pods.json"
registry_pod=$(python3 - "$RUNNER_TEMP/hold-crossplane-pods.json" <<'PYREG'
import json,sys
pods=json.load(open(sys.argv[1]))['items']
ready=[p['metadata']['name'] for p in pods if not p['metadata'].get('deletionTimestamp') and any(c['name']=='ephemeral-registry' and c.get('ready') for c in p.get('status',{}).get('containerStatuses',[]))]
assert len(ready)==1
print(ready[0])
PYREG
)
mkdir -p "$RUNNER_TEMP/hold-registry-data"
"$kubectl" -n upbound-system cp -c ephemeral-registry "$registry_pod:/tmp/registry/." "$RUNNER_TEMP/hold-registry-data"
docker run -d --name hold-registry --network "container:$node" -v "$RUNNER_TEMP/hold-registry-data:/var/lib/registry" -e REGISTRY_HTTP_ADDR=0.0.0.0:5000 "$image" >/dev/null
for attempt in $(seq 1 60); do
 if node_wget -q -T 2 -O /dev/null http://127.0.0.1:5000/v2/; then break; fi
 sleep 1
done
node_wget -q -T 2 -O /dev/null http://127.0.0.1:5000/v2/
curl -fsS -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json' "http://$node_ip:5000/v2/provider-rediscloud/manifests/$digest" -o "$RUNNER_TEMP/hold-registry-manifest.json"
echo "${digest#sha256:}  $RUNNER_TEMP/hold-registry-manifest.json" | sha256sum -c -
docker exec "$node" sh -c 'printf "[host.\"http://127.0.0.1:5000\"]\n" > /etc/containerd/certs.d/registry.localhost:5000/hosts.toml'
# Source-wide default deny, two explicit node destinations. Applies to original
# and replacement pod IPs; disposable single-stack Kind node only.
docker exec "$node" iptables -N CODEX-HOLD
docker exec "$node" iptables -A CODEX-HOLD -s "$cidr" -d "$node_ip" -p tcp --dport 5556 -j ACCEPT
docker exec "$node" iptables -A CODEX-HOLD -s "$cidr" -d "$node_ip" -p tcp --dport 6443 -j ACCEPT
docker exec "$node" iptables -A CODEX-HOLD -s "$cidr" -m comment --comment codex-hold-deny -j REJECT
docker exec "$node" iptables -A CODEX-HOLD -j RETURN
docker exec "$node" iptables -I INPUT 1 -j CODEX-HOLD
docker exec "$node" iptables -I FORWARD 1 -j CODEX-HOLD
probe(){
 local pod=$1 uid before after
 uid=$("$kubectl" -n upbound-system get pod "$pod" -o jsonpath='{.metadata.uid}')
 "$kubectl" -n upbound-system get pod "$pod" -o json | python3 -c 'import ipaddress,json,sys; p=json.load(sys.stdin); ips=[ipaddress.ip_address(x["ip"]) for x in p["status"]["podIPs"]]; assert not p["spec"].get("hostNetwork") and p["spec"]["nodeName"]==sys.argv[1] and len(ips)==1 and ips[0].version==4 and ips[0] in ipaddress.ip_network(sys.argv[2]); containers=[c for c in p["spec"]["containers"] if c["name"]=="package-runtime"]; assert len(containers)==1 and any(e.get("name")=="ENABLE_PRO_DATABASE_DELETE_HOLD" and e.get("value")=="true" for e in containers[0].get("env",[])) and any(e.get("name")=="PROVIDER_DEBUG" and e.get("value")=="true" for e in containers[0].get("env",[]))' "$node" "$cidr"
 "$kubectl" -n upbound-system debug "pod/$pod" --container=fixture-probe --image="$image" --attach=false -- /bin/sh -c 'sleep 1200' >/dev/null
 for attempt in $(seq 1 60); do
  if "$kubectl" -n upbound-system get pod "$pod" -o jsonpath='{.status.ephemeralContainerStatuses[?(@.name=="fixture-probe")].state.running.startedAt}' | grep -q .; then break; fi
  sleep 1
 done
 "$kubectl" -n upbound-system exec "$pod" -c fixture-probe -- wget -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$node_ip:5556/v1/subscriptions/1"
 local pod_ip node_before node_after
 pod_ip=$("$kubectl" -n upbound-system get pod "$pod" -o jsonpath='{.status.podIP}')
 cleanup_node_source=$pod_ip
 docker exec "$node" iptables -I INPUT 1 -s "$pod_ip/32" -d "$node_ip/32" -p tcp --dport 5555 -m comment --comment codex-hold-node-flow
 node_before=$(docker exec "$node" iptables -nvx -L INPUT | awk '/codex-hold-node-flow/ {print $1}')
 before=$(docker exec "$node" iptables -nvx -L CODEX-HOLD | awk '/codex-hold-deny/ {print $1}')
 [[ "$before" =~ ^[0-9]+$ && "$node_before" =~ ^[0-9]+$ ]]
 "$kubectl" -n upbound-system exec "$pod" -c fixture-probe -- /bin/sh -c 'if wget -q -T 3 -O /dev/null "http://$1:5555/v2/"; then exit 42; else exit 0; fi' -- "$node_ip"
 after=$(docker exec "$node" iptables -nvx -L CODEX-HOLD | awk '/codex-hold-deny/ {print $1}')
 node_after=$(docker exec "$node" iptables -nvx -L INPUT | awk '/codex-hold-node-flow/ {print $1}')
 docker exec "$node" iptables -D INPUT -s "$pod_ip/32" -d "$node_ip/32" -p tcp --dport 5555 -m comment --comment codex-hold-node-flow
 cleanup_node_source=''
 [[ "$after" =~ ^[0-9]+$ && "$node_after" =~ ^[0-9]+$ ]] && (( after > before && node_after > node_before ))
 # An exact per-pod allow restores the same target while default deny remains.
 cleanup_node_allow=$pod_ip
 docker exec "$node" iptables -I CODEX-HOLD 1 -s "$pod_ip/32" -d "$node_ip/32" -p tcp --dport 5555 -j ACCEPT
 "$kubectl" -n upbound-system exec "$pod" -c fixture-probe -- wget -q -T 3 -O /dev/null "http://$node_ip:5555/v2/"
 docker exec "$node" iptables -D CODEX-HOLD -s "$pod_ip/32" -d "$node_ip/32" -p tcp --dport 5555 -j ACCEPT
 cleanup_node_allow=''
 node_wget -q -T 2 -O /dev/null http://127.0.0.1:5555/v2/
 # Independent non-node fixture uses FORWARD; attribute its specific flow
 # and the CIDR deny separately so an unrelated failure cannot count as deny.
 local flow_before flow_after deny_before deny_after
 cleanup_source=$pod_ip
 docker exec "$node" iptables -I FORWARD 1 -s "$pod_ip/32" -d "$external_ip/32" -p tcp --dport 5555 -m comment --comment codex-hold-flow
 flow_before=$(docker exec "$node" iptables -nvx -L FORWARD | awk '/codex-hold-flow/ {print $1}')
 deny_before=$(docker exec "$node" iptables -nvx -L CODEX-HOLD | awk '/codex-hold-deny/ {print $1}')
 "$kubectl" -n upbound-system exec "$pod" -c fixture-probe -- /bin/sh -c 'if wget -q -T 3 -O /dev/null "http://$1:5555/v2/"; then exit 42; else exit 0; fi' -- "$external_ip"
 flow_after=$(docker exec "$node" iptables -nvx -L FORWARD | awk '/codex-hold-flow/ {print $1}')
 deny_after=$(docker exec "$node" iptables -nvx -L CODEX-HOLD | awk '/codex-hold-deny/ {print $1}')
 docker exec "$node" iptables -D FORWARD -s "$pod_ip/32" -d "$external_ip/32" -p tcp --dport 5555 -m comment --comment codex-hold-flow
 cleanup_source=''
 [[ "$flow_before" =~ ^[0-9]+$ && "$flow_after" =~ ^[0-9]+$ && "$deny_before" =~ ^[0-9]+$ && "$deny_after" =~ ^[0-9]+$ ]] && ((flow_after>flow_before && deny_after>deny_before))
 cleanup_external_allow=$pod_ip
 docker exec "$node" iptables -I CODEX-HOLD 1 -s "$pod_ip/32" -d "$external_ip/32" -p tcp --dport 5555 -j ACCEPT
 "$kubectl" -n upbound-system exec "$pod" -c fixture-probe -- wget -q -T 3 -O /dev/null "http://$external_ip:5555/v2/"
 docker exec "$node" iptables -D CODEX-HOLD -s "$pod_ip/32" -d "$external_ip/32" -p tcp --dport 5555 -j ACCEPT
 cleanup_external_allow=''
 node_wget -q -T 2 -O /dev/null "http://$external_ip:5555/v2/"
 test "$("$kubectl" -n upbound-system get pod "$pod" -o jsonpath='{.metadata.uid}')" = "$uid"
}
probe "$original"
# Fixed dummy credentials, fixed fake origin; no Cloud account material.
python3 - "$node_ip" <<'PY' | "$kubectl" apply -f -
import json,sys
cred=json.dumps({'api_key':'fixture-key','secret_key':'fixture-secret','url':f'http://{sys.argv[1]}:5556/v1'})
items=[{'apiVersion':'v1','kind':'Secret','metadata':{'name':'hold-fixture','namespace':'upbound-system'},'stringData':{'credentials':cred}},
{'apiVersion':'rediscloud.redis.io/v1beta1','kind':'ProviderConfig','metadata':{'name':'hold-fixture'},'spec':{'credentials':{'source':'Secret','secretRef':{'name':'hold-fixture','namespace':'upbound-system','key':'credentials'}}}},
{'apiVersion':'subscription.redis.io/v1beta1','kind':'Database','metadata':{'name':'hold-fixture'},'spec':{'providerConfigRef':{'name':'hold-fixture'},'forProvider':{'subscriptionId':1,'name':'fake-database','dataEviction':'allkeys-lru','memoryLimitInGb':1,'throughputMeasurementBy':'operations-per-second','throughputMeasurementValue':1000,'dataPersistence':'none','replication':False}}}]
json.dump({'apiVersion':'v1','kind':'List','items':items},sys.stdout)
PY
for attempt in $(seq 1 180); do
 ext=$("$kubectl" get databases.subscription.redis.io hold-fixture -o jsonpath='{.metadata.annotations.crossplane\.io/external-name}' || true)
 [[ "$ext" == '1/42' ]] && break
 sleep 2
done
[[ "$ext" == '1/42' ]]
cr_uid=$("$kubectl" get databases.subscription.redis.io hold-fixture -o jsonpath='{.metadata.uid}')
old_uid=$("$kubectl" -n upbound-system get pod "$original" -o jsonpath='{.metadata.uid}')
old_ip=$("$kubectl" -n upbound-system get pod "$original" -o jsonpath='{.status.podIP}')
old_id=$("$kubectl" -n upbound-system get pod "$original" -o jsonpath='{.status.containerStatuses[?(@.name=="package-runtime")].containerID}')
old_id=${old_id#containerd://}
[[ "$old_id" =~ ^[a-f0-9]{64}$ ]]
docker exec "$node" crictl ps -o json > "$RUNNER_TEMP/hold-runtime-before.json"
python3 - "$RUNNER_TEMP/hold-runtime-before.json" "$old_id" <<'PYRUN'
import json,sys
x=json.load(open(sys.argv[1])); assert isinstance(x.get('containers'),list) and any(c.get('id')==sys.argv[2] for c in x['containers'])
PYRUN
"$kubectl" delete databases.subscription.redis.io hold-fixture --wait=false
for attempt in $(seq 1 180); do
 state=$(node_wget -q -T 2 -O - --header='X-Fixture-Control: fixture-control' http://127.0.0.1:5556/fixture/state)
 if python3 -c 'import json,sys; x=json.load(sys.stdin); sys.exit(0 if x["creates"]==1 and x["deletes"]==1 and x["exists"] and x["unknown"]==0 else 1)' <<<"$state"; then break; fi
 sleep 2
done
python3 -c 'import json,sys; x=json.load(sys.stdin); assert x["creates"]==1 and x["deletes"]==1 and x["exists"] and x["unknown"]==0 and x["deleteSource"]==sys.argv[1]' "$old_ip" <<<"$state"
"$kubectl" get databases.subscription.redis.io hold-fixture -o json | python3 -c 'import json,sys; x=json.load(sys.stdin); assert "rediscloud.crossplane.io/pro-database-delete-intent" in x["metadata"].get("annotations",{}) and x["metadata"].get("finalizers")'
# Confirm the original provider identity is unchanged while the fake request is held.
test "$("$kubectl" -n upbound-system get pod "$original" -o jsonpath='{.metadata.uid}')" = "$old_uid"
test "$("$kubectl" -n upbound-system get pod "$original" -o jsonpath='{.status.containerStatuses[?(@.name=="package-runtime")].containerID}')" = "containerd://$old_id"
# Pod force deletion is not labeled SIGKILL. Require the old container absent
# from the node's running runtime inventory before unblocking the fake server.
"$kubectl" -n upbound-system delete pod "$original" --grace-period=0 --force --wait=false
old_gone=false
for attempt in $(seq 1 90); do
 docker exec "$node" crictl ps -o json > "$RUNNER_TEMP/hold-runtime.json"
 if python3 - "$RUNNER_TEMP/hold-runtime.json" "$old_id" <<'PYRUN'
import json,sys
x=json.load(open(sys.argv[1])); assert isinstance(x.get('containers'),list)
sys.exit(1 if any(c.get('id')==sys.argv[2] for c in x['containers']) else 0)
PYRUN
 then old_gone=true; break; fi
 sleep 1
done
[[ "$old_gone" == true ]] || { echo 'old runtime still running' >&2; exit 1; }
replacement=''
for attempt in $(seq 1 180); do
 "$kubectl" -n upbound-system get pods -o json > "$RUNNER_TEMP/hold-pods.json"
 replacement=$(python3 - "$RUNNER_TEMP/hold-pods.json" "$old_uid" "$digest" <<'PY'
import json,sys
pods=json.load(open(sys.argv[1]))['items']
matches=[p for p in pods if p['metadata']['name'].startswith('provider-rediscloud-') and p['metadata']['uid']!=sys.argv[2] and not p['metadata'].get('deletionTimestamp') and any(c['name']=='package-runtime' and c.get('ready') and c.get('imageID','').rsplit('@',1)[-1]==sys.argv[3] for c in p.get('status',{}).get('containerStatuses',[]))]
if len(matches)==1: print(matches[0]['metadata']['name'])
PY
)
 [[ -n "$replacement" ]] && break
 sleep 2
done
if [[ -z "$replacement" ]]; then
 python3 - "$RUNNER_TEMP/hold-pods.json" <<'PYDIAG'
import json,sys
for p in json.load(open(sys.argv[1]))['items']:
 if p['metadata']['name'].startswith('provider-rediscloud-'):
  states=[{'name':c['name'],'ready':c.get('ready',False),'reason':c.get('state',{}).get('waiting',{}).get('reason','')} for c in p.get('status',{}).get('containerStatuses',[])]
  print({'phase':p.get('status',{}).get('phase'),'containers':states})
PYDIAG
 echo 'replacement provider did not become ready' >&2; exit 1
fi
probe "$replacement"
# CR type does not implement the optional lastHandledReconcileAt tracker.
# After the old runtime has gone and exactly one replacement is ready, require
# a fresh guard event for this exact CR and a matching error condition. Event
# count AND reporter timestamp must advance after a new annotation request.
replacement_uid=$("$kubectl" -n upbound-system get pod "$replacement" -o jsonpath='{.metadata.uid}')
# The event recorder does not include a Pod identity; baseline and compare
# per event UID, and separately require a fresh guard log from the replacement.
log_count(){
 "$kubectl" -n upbound-system logs "$replacement" -c package-runtime | python3 -c 'import sys; uid=sys.argv[1]; print(sum(uid in x and "pro database delete is held" in x for x in sys.stdin))' "$cr_uid"
}
sleep 2
"$kubectl" get events --field-selector "involvedObject.uid=$cr_uid" -o json > "$RUNNER_TEMP/hold-event-baseline.json"
# kingpin DefaultEnvars prefixes debug with the provider executable name.
# Verify actual debug output without printing the full fake-only logs.
"$kubectl" -n upbound-system logs "$replacement" -c package-runtime | python3 -c 'import sys; lines=list(sys.stdin); assert any("Starting" in x and "sync-period" in x for x in lines)'
baseline_logs=$(log_count)
[[ "$baseline_logs" =~ ^[0-9]+$ ]]
cutoff=$(date -u +%Y-%m-%dT%H:%M:%SZ)
token="hold-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
"$kubectl" annotate databases.subscription.redis.io hold-fixture --overwrite "crossplane.io/reconcile-requested-at=$token"
handled=false
for attempt in $(seq 1 120); do
 "$kubectl" get databases.subscription.redis.io hold-fixture -o json > "$RUNNER_TEMP/hold-status.json"
 "$kubectl" get events --field-selector "involvedObject.uid=$cr_uid" -o json > "$RUNNER_TEMP/hold-events.json"
 current_logs=$(log_count)
 [[ "$current_logs" =~ ^[0-9]+$ ]]
 if python3 - "$RUNNER_TEMP/hold-status.json" "$RUNNER_TEMP/hold-events.json" "$RUNNER_TEMP/hold-event-baseline.json" "$cr_uid" "$baseline_logs" "$current_logs" "$cutoff" <<'PYHOLD'
import copy,datetime,json,sys
obj=json.load(open(sys.argv[1])); events=json.load(open(sys.argv[2]))['items']; baseline=json.load(open(sys.argv[3]))['items']; cutoff=datetime.datetime.fromisoformat(sys.argv[7].replace('Z','+00:00'))
def count(e): return max(e.get('count',1), (e.get('series') or {}).get('count',0))
def matching(e,uid): return e.get('involvedObject',{}).get('uid')==uid and e.get('reason')=='CannotConnectToProvider' and 'pro database delete is held' in e.get('message','')
def timestamp(e):
 value=(e.get('series') or {}).get('lastObservedTime') or e.get('lastTimestamp') or e.get('eventTime')
 return datetime.datetime.fromisoformat(value.replace('Z','+00:00')) if value else datetime.datetime.min.replace(tzinfo=datetime.timezone.utc)
def valid(obj,items,old,uid,before_logs,after_logs,threshold):
 status=obj.get('status',{})
 if obj['metadata']['uid']!=uid or not any(c.get('type')=='Synced' and c.get('status')=='False' and 'pro database delete is held' in c.get('message','') for c in status.get('conditions',[])) or after_logs<=before_logs: return False
 old_counts={e['metadata']['uid']:count(e) for e in old if matching(e,uid)}
 return any(count(e)>old_counts.get(e['metadata']['uid'],0) and timestamp(e)>=threshold for e in items if matching(e,uid))
uid=sys.argv[4]; before=int(sys.argv[5]); after=int(sys.argv[6])
assert not valid(obj,events,baseline,'wrong-uid',before,after,cutoff)
assert not valid(obj,events,events,uid,before,after,cutoff)
assert not valid(obj,events,baseline,uid,before,before,cutoff)
assert not valid(obj,events,baseline,uid,before,after,datetime.datetime.max.replace(tzinfo=datetime.timezone.utc))
# A fresh timestamp on a different unchanged event cannot validate an old increment.
old_time=(cutoff-datetime.timedelta(seconds=30)).isoformat(); future_time=(cutoff+datetime.timedelta(seconds=30)).isoformat()
old_event={'metadata':{'uid':'old'},'involvedObject':{'uid':uid},'reason':'CannotConnectToProvider','message':'pro database delete is held','count':1,'lastTimestamp':old_time}
fresh_event=copy.deepcopy(old_event); fresh_event['metadata']['uid']='fresh'; fresh_event['lastTimestamp']=future_time
changed=copy.deepcopy(old_event); changed['count']=2
assert not valid(obj,[changed,fresh_event],[old_event,fresh_event],uid,before,before+1,cutoff)
sys.exit(0 if valid(obj,events,baseline,uid,before,after,cutoff) else 1)
PYHOLD
 then handled=true; break; fi
 sleep 2
done
if [[ "$handled" != true ]]; then
 python3 - "$RUNNER_TEMP/hold-status.json" "$RUNNER_TEMP/hold-events.json" "$baseline_logs" "$current_logs" <<'PYDIAG'
import json,sys
s=json.load(open(sys.argv[1])).get('status',{}); events=json.load(open(sys.argv[2]))['items']
print({'baselineLogs':sys.argv[3], 'currentLogs':sys.argv[4], 'conditions':[{'type':c.get('type'),'status':c.get('status'),'reason':c.get('reason')} for c in s.get('conditions',[])], 'eventReasons':[e.get('reason') for e in events]})
PYDIAG
 echo 'replacement did not provide fresh held reconcile evidence' >&2; exit 1
fi
test "$("$kubectl" -n upbound-system get pod "$replacement" -o jsonpath='{.metadata.uid}')" = "$replacement_uid"
node_wget -q -T 2 -O /dev/null --post-data='' --header='X-Fixture-Control: fixture-control' http://127.0.0.1:5556/fixture/release
# Bounded repeated reconciliation/observation is scoped to this fake run.
for attempt in $(seq 1 12); do
 "$kubectl" get databases.subscription.redis.io hold-fixture -o json | python3 -c 'import json,sys; x=json.load(sys.stdin); assert x["metadata"]["uid"]==sys.argv[1] and "rediscloud.crossplane.io/pro-database-delete-intent" in x["metadata"].get("annotations",{}) and x["metadata"].get("finalizers")' "$cr_uid"
 state=$(node_wget -q -T 2 -O - --header='X-Fixture-Control: fixture-control' http://127.0.0.1:5556/fixture/state)
 python3 -c 'import json,sys; x=json.load(sys.stdin); assert x["creates"]==1 and x["deletes"]==1 and x["exists"] and x["unknown"]==0' <<<"$state"
 sleep 5
done
echo 'Fake DELETE held across exact provider Pod replacement; no live Cloud qualification.'

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
 docker rm -f hold-fake hold-denied hold-external >/dev/null 2>&1 || true
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
 "$kubectl" -n upbound-system get pod "$pod" -o json | python3 -c 'import ipaddress,json,sys; p=json.load(sys.stdin); ips=[ipaddress.ip_address(x["ip"]) for x in p["status"]["podIPs"]]; assert not p["spec"].get("hostNetwork") and p["spec"]["nodeName"]==sys.argv[1] and len(ips)==1 and ips[0].version==4 and ips[0] in ipaddress.ip_network(sys.argv[2]); containers=[c for c in p["spec"]["containers"] if c["name"]=="package-runtime"]; assert len(containers)==1 and any(e.get("name")=="ENABLE_PRO_DATABASE_DELETE_HOLD" and e.get("value")=="true" for e in containers[0].get("env",[]))' "$node" "$cidr"
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
[[ -n "$replacement" ]]
probe "$replacement"
# Require the replacement controller to observe a fresh explicit reconcile token
# and report the hold; a merely ready but idle replacement is not sufficient.
token="hold-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
"$kubectl" annotate databases.subscription.redis.io hold-fixture --overwrite "crossplane.io/reconcile-requested-at=$token"
handled=false
for attempt in $(seq 1 120); do
 "$kubectl" get databases.subscription.redis.io hold-fixture -o json > "$RUNNER_TEMP/hold-status.json"
 if python3 - "$RUNNER_TEMP/hold-status.json" "$cr_uid" "$token" <<'PYHOLD'
import json,sys
x=json.load(open(sys.argv[1])); s=x.get('status',{})
assert x['metadata']['uid']==sys.argv[2]
assert s.get('lastHandledReconcileAt')==sys.argv[3]
assert any(c.get('type')=='Synced' and c.get('status')=='False' and 'pro database delete is held' in c.get('message','') for c in s.get('conditions',[]))
PYHOLD
 then handled=true; break; fi
 sleep 2
done
[[ "$handled" == true ]] || { echo 'replacement did not report the held reconcile' >&2; exit 1; }
node_wget -q -T 2 -O /dev/null --post-data='' --header='X-Fixture-Control: fixture-control' http://127.0.0.1:5556/fixture/release
# Bounded repeated reconciliation/observation is scoped to this fake run.
for attempt in $(seq 1 12); do
 "$kubectl" get databases.subscription.redis.io hold-fixture -o json | python3 -c 'import json,sys; x=json.load(sys.stdin); assert x["metadata"]["uid"]==sys.argv[1] and "rediscloud.crossplane.io/pro-database-delete-intent" in x["metadata"].get("annotations",{}) and x["metadata"].get("finalizers")' "$cr_uid"
 state=$(node_wget -q -T 2 -O - --header='X-Fixture-Control: fixture-control' http://127.0.0.1:5556/fixture/state)
 python3 -c 'import json,sys; x=json.load(sys.stdin); assert x["creates"]==1 and x["deletes"]==1 and x["exists"] and x["unknown"]==0' <<<"$state"
 sleep 5
done
echo 'Fake DELETE held across exact provider Pod replacement; no live Cloud qualification.'

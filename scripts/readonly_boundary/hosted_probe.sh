#!/usr/bin/env bash
# Credential-free experiment in a disposable Actions Kind cluster only.
set -euo pipefail
[[ "${GITHUB_ACTIONS:-}" == true ]] || { echo 'hosted Actions required' >&2; exit 1; }
kubectl=.cache/tools/linux_x86_64/kubectl-v1.33.5
pod=$(cat "$RUNNER_TEMP/provider-pod.txt")
node=local-dev-control-plane
image='registry:2.8.3@sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373'
"$kubectl" get node "$node" -o json > "$RUNNER_TEMP/boundary-node.json"
python3 - "$RUNNER_TEMP/boundary-node.json" "$RUNNER_TEMP/boundary-node.env" <<'PY'
import ipaddress, json, pathlib, sys
node = json.loads(pathlib.Path(sys.argv[1]).read_text())
ips = [ipaddress.ip_address(x['address']) for x in node['status']['addresses'] if x['type'] == 'InternalIP']
cidrs = [ipaddress.ip_network(x) for x in node['spec']['podCIDRs']]
values = {}
for family in (4, 6):
    family_ips = [str(x) for x in ips if x.version == family]
    family_cidrs = [str(x) for x in cidrs if x.version == family]
    if len(family_ips) != 1 or len(family_cidrs) != 1:
        raise SystemExit('expected one node IP and pod CIDR for each address family')
    values[f'ip{family}'], values[f'cidr{family}'] = family_ips[0], family_cidrs[0]
pathlib.Path(sys.argv[2]).write_text(''.join(f'{k}={v}\n' for k, v in values.items()))
PY
source "$RUNNER_TEMP/boundary-node.env"
"$kubectl" -n upbound-system get pod "$pod" -o json > "$RUNNER_TEMP/boundary-pod.json"
python3 - "$RUNNER_TEMP/boundary-pod.json" "$cidr4" "$cidr6" <<'PY'
import ipaddress, json, pathlib, sys
p = json.loads(pathlib.Path(sys.argv[1]).read_text())
if p['spec'].get('hostNetwork') or p['spec']['nodeName'] != 'local-dev-control-plane':
    raise SystemExit('provider must be on the guarded node and not host network')
ips = [ipaddress.ip_address(x['ip']) for x in p['status']['podIPs']]
if {i.version for i in ips} != {4, 6} or any(not any(i in ipaddress.ip_network(c) for c in sys.argv[2:]) for i in ips):
    raise SystemExit('provider does not have guarded dual-stack pod addresses')
PY
cleanup() {
  for tool in iptables ip6tables; do
    for chain in INPUT FORWARD; do
      docker exec "$node" "$tool" -D "$chain" -j CODEX-EGRESS 2>/dev/null || true
      docker exec "$node" "$tool" -F CODEX-EGRESS 2>/dev/null || true
      docker exec "$node" "$tool" -X CODEX-EGRESS 2>/dev/null || true
    done
  done
  docker stop boundary-proxy4 boundary-proxy6 boundary-denied4 boundary-denied6 >/dev/null 2>&1 || true
}
trap cleanup EXIT
CGO_ENABLED=0 go build -o "$RUNNER_TEMP/boundary-proxy" ./proof-source/scripts/readonly_boundary/fake_proxy.go
for family in 4 6; do
  if [ "$family" = 4 ]; then proxy_addr=0.0.0.0:5556; denied_addr=0.0.0.0:5555; else proxy_addr='[::]:5556'; denied_addr='[::]:5555'; fi
  docker run -d --rm --name "boundary-proxy$family" --network "container:$node" -v "$RUNNER_TEMP/boundary-proxy:/fixture:ro" -e "LISTEN_ADDRESS=$proxy_addr" --entrypoint /fixture "$image" >/dev/null
  docker run -d --rm --name "boundary-denied$family" --network "container:$node" -e "REGISTRY_HTTP_ADDR=$denied_addr" "$image" >/dev/null
done
# Confirm all positive and negative targets are independently reachable first.
for family in 4 6; do
  if [ "$family" = 4 ]; then host=127.0.0.1; else host='[::1]'; fi
  docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$host:5556/regions"
  docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 3 -O /dev/null "http://$host:5555/v2/"
done
# CIDR-wide rules apply before a replacement pod can start, not just to an old IP.
for family in 4 6; do
  if [ "$family" = 4 ]; then tool=iptables; cidr=$cidr4; node_ip=$ip4; else tool=ip6tables; cidr=$cidr6; node_ip=$ip6; fi
  docker exec "$node" "$tool" -N CODEX-EGRESS
  docker exec "$node" "$tool" -A CODEX-EGRESS -s "$cidr" -d "$node_ip" -p tcp --dport 5556 -j ACCEPT
  docker exec "$node" "$tool" -A CODEX-EGRESS -s "$cidr" -d "$node_ip" -p tcp --dport 6443 -j ACCEPT
  docker exec "$node" "$tool" -A CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
  docker exec "$node" "$tool" -A CODEX-EGRESS -j RETURN
  docker exec "$node" "$tool" -I INPUT 1 -j CODEX-EGRESS
  docker exec "$node" "$tool" -I FORWARD 1 -j CODEX-EGRESS
  docker exec "$node" "$tool" -C INPUT -j CODEX-EGRESS
  docker exec "$node" "$tool" -C FORWARD -j CODEX-EGRESS
done
probe() {
  local candidate=$1 expected_uid=$2
  "$kubectl" -n upbound-system debug "pod/$candidate" --container=boundary-probe --image="$image" --attach=false -- /bin/sh -c 'sleep 1200'
  for attempt in $(seq 1 60); do
    if "$kubectl" -n upbound-system get pod "$candidate" -o jsonpath='{.status.ephemeralContainerStatuses[?(@.name=="boundary-probe")].state.running.startedAt}' | grep -q .; then break; fi
    sleep 1
  done
  "$kubectl" -n upbound-system get pod "$candidate" -o json > "$RUNNER_TEMP/current-boundary-pod.json"
  for family in 4 6; do
    if [ "$family" = 4 ]; then host=$ip4; target=$ip4; cidr=$cidr4; tool=iptables; else host="[$ip6]"; target=$ip6; cidr=$cidr6; tool=ip6tables; fi
    source_ip=$(python3 - "$RUNNER_TEMP/current-boundary-pod.json" "$family" "$cidr" <<'PYTHON'
import ipaddress, json, pathlib, sys
pod = json.loads(pathlib.Path(sys.argv[1]).read_text())
values = [ipaddress.ip_address(item['ip']) for item in pod['status']['podIPs'] if ipaddress.ip_address(item['ip']).version == int(sys.argv[2])]
if len(values) != 1 or values[0] not in ipaddress.ip_network(sys.argv[3]):
    raise SystemExit('unexpected pod address for firewall probe')
print(values[0])
PYTHON
    )
    "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- wget -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$host:5556/regions"
    "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- /bin/sh -c 'result=$(wget -S -O /dev/null --post-data=x --header="X-Api-Key: fixture-key" --header="X-Api-Secret-Key: fixture-secret" "http://$1:5556/regions" 2>&1); case "$result" in *"403 Forbidden"*) exit 0;; *) exit 1;; esac' -- "$host"
    mapfile -t before < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-cidr-deny-$family" 'index($0, marker) {print $1}')
    test "${#before[@]}" -eq 1 && [[ "${before[0]}" =~ ^[0-9]+$ ]]
    "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- /bin/sh -c 'if wget -q -T 3 -O /dev/null "http://$1:5555/v2/"; then exit 42; else exit 0; fi' -- "$host"
    mapfile -t after < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-cidr-deny-$family" 'index($0, marker) {print $1}')
    test "${#after[@]}" -eq 1 && [[ "${after[0]}" =~ ^[0-9]+$ ]] && (( after[0] > before[0] ))
    # A negative control must fail the overall test if another rule, an
    # unavailable listener, or a broken probe explains the observed denial.
    docker exec "$node" "$tool" -D CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
    "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- wget -q -T 3 -O /dev/null "http://$host:5555/v2/"
    docker exec "$node" "$tool" -I CODEX-EGRESS 3 -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
    docker exec "$node" "$tool" -C CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
  done
  test "$("$kubectl" -n upbound-system get pod "$candidate" -o jsonpath='{.metadata.uid}')" = "$expected_uid"
}
uid=$("$kubectl" -n upbound-system get pod "$pod" -o jsonpath='{.metadata.uid}')
probe "$pod" "$uid"
"$kubectl" -n upbound-system delete pod "$pod" --wait=true
for attempt in $(seq 1 120); do
  "$kubectl" -n upbound-system get pods -o json > "$RUNNER_TEMP/boundary-pods.json"
  if python3 - "$RUNNER_TEMP/boundary-pods.json" "$uid" "$cidr4" "$cidr6" "$RUNNER_TEMP/replacement-name" <<'PY'
import ipaddress, json, pathlib, sys
pods = json.loads(pathlib.Path(sys.argv[1]).read_text())['items']
matching = [p for p in pods if p['metadata']['name'].startswith('provider-rediscloud-') and not p['metadata'].get('deletionTimestamp') and p['metadata']['uid'] != sys.argv[2] and p['spec'].get('nodeName') == 'local-dev-control-plane' and not p['spec'].get('hostNetwork') and any(c['name'] == 'package-runtime' and c.get('ready') for c in p.get('status', {}).get('containerStatuses', []))]
if len(matching) != 1:
    raise SystemExit(1)
p = matching[0]
ips = [ipaddress.ip_address(x['ip']) for x in p['status'].get('podIPs', [])]
if {i.version for i in ips} != {4, 6} or any(not any(i in ipaddress.ip_network(c) for c in sys.argv[3:5]) for i in ips):
    raise SystemExit(1)
pathlib.Path(sys.argv[5]).write_text(p['metadata']['name'])
PY
  then break; fi
  sleep 2
done
replacement=$(cat "$RUNNER_TEMP/replacement-name")
new_uid=$("$kubectl" -n upbound-system get pod "$replacement" -o jsonpath='{.metadata.uid}')
test "$new_uid" != "$uid"
probe "$replacement" "$new_uid"
echo 'Credential-free IPv4/IPv6 node proxy and node-denial probes passed for the original and replacement provider pods. This does not establish full API/RBAC or live QA safety.'

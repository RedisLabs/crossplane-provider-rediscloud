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
# The existing debug container shares the installed provider's network. Pin
# the one solicited-node multicast destination for its IPv6 gateway; never
# turn this fixture-only exception into a general ICMPv6 allowance.
"$kubectl" -n upbound-system exec "$pod" -c node-probe -- ip -6 route > "$RUNNER_TEMP/provider-ipv6-route"
gateway_multicast=$(python3 - "$RUNNER_TEMP/provider-ipv6-route" "$cidr6" <<'PY'
import ipaddress, pathlib, sys
lines = pathlib.Path(sys.argv[1]).read_text().splitlines()
gateways = [line.split()[2] for line in lines if line.startswith('default via ')]
if len(gateways) != 1:
    raise SystemExit('expected one IPv6 gateway')
gateway = ipaddress.IPv6Address(gateways[0])
if gateway not in ipaddress.IPv6Network(sys.argv[2]):
    raise SystemExit('gateway is outside the guarded IPv6 pod CIDR')
print(ipaddress.IPv6Address(int(ipaddress.IPv6Address('ff02::1:ff00:0')) | (int(gateway) & 0xffffff)))
PY
)
cleanup() {
  for tool in iptables ip6tables; do
    docker exec "$node" "$tool" -D FORWARD -j CODEX-FORWARD-COUNT 2>/dev/null || true
    docker exec "$node" "$tool" -F CODEX-FORWARD-COUNT 2>/dev/null || true
    docker exec "$node" "$tool" -X CODEX-FORWARD-COUNT 2>/dev/null || true
    for chain in INPUT FORWARD; do
      docker exec "$node" "$tool" -D "$chain" -j CODEX-EGRESS 2>/dev/null || true
      docker exec "$node" "$tool" -F CODEX-EGRESS 2>/dev/null || true
      docker exec "$node" "$tool" -X CODEX-EGRESS 2>/dev/null || true
    done
  done
  docker rm -f boundary-proxy boundary-denied4 boundary-denied6 boundary-registry boundary-external >/dev/null 2>&1 || true
}
trap cleanup EXIT
# Mirror the already-built package onto the node. The existing registry is a
# pod in the guarded CIDR, so kubelet may be unable to pull from it after the
# guard is installed. The mirror is reachable by kubelet but is not allowed
# to the provider pod by the firewall below.
"$kubectl" -n upbound-system get pods -l app=crossplane -o json > "$RUNNER_TEMP/crossplane-pods.json"
python3 - "$RUNNER_TEMP/crossplane-pods.json" "$RUNNER_TEMP/registry-pod" <<'PY'
import json, pathlib, sys
pods = json.loads(pathlib.Path(sys.argv[1]).read_text())['items']
ready = [p['metadata']['name'] for p in pods if not p['metadata'].get('deletionTimestamp') and any(c['name'] == 'ephemeral-registry' and c.get('ready') for c in p.get('status', {}).get('containerStatuses', []))]
if len(ready) != 1:
    raise SystemExit('expected one ready ephemeral registry')
pathlib.Path(sys.argv[2]).write_text(ready[0])
PY
registry_pod=$(cat "$RUNNER_TEMP/registry-pod")
mkdir -p "$RUNNER_TEMP/boundary-registry-data"
"$kubectl" -n upbound-system cp -c ephemeral-registry "$registry_pod:/tmp/registry/." "$RUNNER_TEMP/boundary-registry-data"
docker run -d --rm --name boundary-registry --network "container:$node" -v "$RUNNER_TEMP/boundary-registry-data:/var/lib/registry" -e REGISTRY_HTTP_ADDR=0.0.0.0:5000 "$image" >/dev/null
for attempt in $(seq 1 60); do
  if docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 2 -O /dev/null http://127.0.0.1:5000/v2/; then break; fi
  sleep 1
done
docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 2 -O /dev/null http://127.0.0.1:5000/v2/
digest=$(python3 - "$RUNNER_TEMP/boundary-pod.json" <<'PY'
import json, pathlib, re, sys
pod = json.loads(pathlib.Path(sys.argv[1]).read_text())
ids = [c['imageID'].rsplit('@', 1)[-1] for c in pod['status']['containerStatuses'] if c['name'] == 'package-runtime' and c.get('ready')]
if len(ids) != 1 or not re.fullmatch(r'sha256:[0-9a-f]{64}', ids[0]):
    raise SystemExit('missing immutable runtime digest')
print(ids[0])
PY
)
curl -fsS -H 'Accept: application/vnd.oci.image.index.v1+json, application/vnd.oci.image.manifest.v1+json, application/vnd.docker.distribution.manifest.v2+json, application/vnd.docker.distribution.manifest.list.v2+json' "http://$ip4:5000/v2/provider-rediscloud/manifests/$digest" -o "$RUNNER_TEMP/boundary-registry-manifest.json"
echo "${digest#sha256:}  $RUNNER_TEMP/boundary-registry-manifest.json" | sha256sum -c -
docker exec "$node" sh -c 'printf "[host.\"http://127.0.0.1:5000\"]\n" > /etc/containerd/certs.d/registry.localhost:5000/hosts.toml'
CGO_ENABLED=0 go build -o "$RUNNER_TEMP/boundary-proxy" ./proof-source/scripts/readonly_boundary/fake_proxy.go
docker run -d --name boundary-proxy --network "container:$node" -v "$RUNNER_TEMP/boundary-proxy:/fixture:ro" -e LISTEN_ADDRESS=:5556 --entrypoint /fixture "$image" >/dev/null
# An independent fake endpoint on the Kind bridge exercises forwarded traffic
# to a non-node destination. It has no Cloud upstream and only fake keys.
docker run -d --name boundary-external --network kind -v "$RUNNER_TEMP/boundary-proxy:/fixture:ro" -e LISTEN_ADDRESS=:5556 --entrypoint /fixture "$image" >/dev/null
docker inspect boundary-external > "$RUNNER_TEMP/boundary-external.json"
python3 - "$RUNNER_TEMP/boundary-external.json" "$RUNNER_TEMP/boundary-external.env" "$ip4" "$ip6" <<'PY'
import ipaddress, json, pathlib, sys
items = json.loads(pathlib.Path(sys.argv[1]).read_text())
if len(items) != 1:
    raise SystemExit('expected one fake external endpoint')
network = items[0]['NetworkSettings']['Networks']['kind']
v4 = ipaddress.IPv4Address(network['IPAddress'])
v6 = ipaddress.IPv6Address(network['GlobalIPv6Address'])
if v4 == ipaddress.ip_address(sys.argv[3]) or v6 == ipaddress.ip_address(sys.argv[4]):
    raise SystemExit('fake external endpoint must not be the node')
pathlib.Path(sys.argv[2]).write_text(f'external4={v4}\nexternal6={v6}\n')
PY
source "$RUNNER_TEMP/boundary-external.env"
for family in 4 6; do
  if [ "$family" = 4 ]; then denied_addr=0.0.0.0:5555; else denied_addr='[::]:5555'; fi
  docker run -d --rm --name "boundary-denied$family" --network "container:$node" -e "REGISTRY_HTTP_ADDR=$denied_addr" "$image" >/dev/null
done
# Both the node network and actual provider pod must reach the non-node fake
# endpoint before default denial. An unavailable fixture cannot prove denial.
for family in 4 6; do
  if [ "$family" = 4 ]; then external_host=$external4; else external_host="[$external6]"; fi
  for attempt in $(seq 1 10); do
    if docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$external_host:5556/regions"; then break; fi
    sleep 1
  done
  docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$external_host:5556/regions"
  "$kubectl" -n upbound-system exec "$pod" -c node-probe -- wget -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$external_host:5556/regions"
done
# Confirm all positive and negative targets are independently reachable first.
for family in 4 6; do
  if [ "$family" = 4 ]; then host=127.0.0.1; else host='[::1]'; fi
  docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$host:5556/regions"
  docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 3 -O /dev/null "http://$host:5555/v2/"
done
# Establish that this specific node responds to IPv6 echo before rules; a
# blocked ping later is meaningful only when this independent baseline works.
"$kubectl" -n upbound-system exec "$pod" -c node-probe -- ping -6 -c 1 -W 3 "$ip6" >/dev/null
# CIDR-wide rules apply before a replacement pod can start, not just to an old IP.
for family in 4 6; do
  if [ "$family" = 4 ]; then tool=iptables; cidr=$cidr4; node_ip=$ip4; else tool=ip6tables; cidr=$cidr6; node_ip=$ip6; fi
  docker exec "$node" "$tool" -N CODEX-EGRESS
  docker exec "$node" "$tool" -A CODEX-EGRESS -s "$cidr" -d "$node_ip" -p tcp --dport 5556 -j ACCEPT
  docker exec "$node" "$tool" -A CODEX-EGRESS -s "$cidr" -d "$node_ip" -p tcp --dport 6443 -j ACCEPT
  if [ "$family" = 6 ]; then
    # Nonterminating accounting only: do not relax IPv6 Neighbor Discovery.
    docker exec "$node" "$tool" -A CODEX-EGRESS -p ipv6-icmp --icmpv6-type 135 -m comment --comment codex-icmpv6-ns
    docker exec "$node" "$tool" -A CODEX-EGRESS -p ipv6-icmp --icmpv6-type 136 -m comment --comment codex-icmpv6-na
    # Fake-only experiment: permit gateway neighbor solicitation, not general
    # ICMPv6. Verify the replacement actually uses this exact exception.
    docker exec "$node" "$tool" -A CODEX-EGRESS -s "$cidr" -d "$gateway_multicast/128" -p ipv6-icmp --icmpv6-type 135 -m hl --hl-eq 255 -m comment --comment codex-gateway-ns -j ACCEPT
  fi
  docker exec "$node" "$tool" -A CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
  docker exec "$node" "$tool" -A CODEX-EGRESS -j RETURN
  docker exec "$node" "$tool" -I INPUT 1 -j CODEX-EGRESS
  docker exec "$node" "$tool" -I FORWARD 1 -j CODEX-EGRESS
  docker exec "$node" "$tool" -N CODEX-FORWARD-COUNT
  docker exec "$node" "$tool" -I FORWARD 1 -j CODEX-FORWARD-COUNT
  docker exec "$node" "$tool" -C INPUT -j CODEX-EGRESS
  docker exec "$node" "$tool" -C FORWARD -j CODEX-EGRESS
done
docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 2 -O /dev/null http://127.0.0.1:5000/v2/
probe() {
  local candidate=$1 expected_uid=$2 phase=$3
  "$kubectl" -n upbound-system debug "pod/$candidate" --container=boundary-probe --image="$image" --attach=false -- /bin/sh -c 'sleep 1200'
  for attempt in $(seq 1 60); do
    if "$kubectl" -n upbound-system get pod "$candidate" -o jsonpath='{.status.ephemeralContainerStatuses[?(@.name=="boundary-probe")].state.running.startedAt}' | grep -q .; then break; fi
    sleep 1
  done
  "$kubectl" -n upbound-system get pod "$candidate" -o json > "$RUNNER_TEMP/current-boundary-pod.json"
  for family in 4 6; do
    if [ "$family" = 4 ]; then host=$ip4; target=$ip4; cidr=$cidr4; tool=iptables; else host="[$ip6]"; target=$ip6; cidr=$cidr6; tool=ip6tables; fi
    if [ "$family" = 4 ]; then external_host=$external4; external_target=$external4; else external_host="[$external6]"; external_target=$external6; fi
    source_ip=$(python3 - "$RUNNER_TEMP/current-boundary-pod.json" "$family" "$cidr" <<'PYTHON'
import ipaddress, json, pathlib, sys
pod = json.loads(pathlib.Path(sys.argv[1]).read_text())
values = [ipaddress.ip_address(item['ip']) for item in pod['status']['podIPs'] if ipaddress.ip_address(item['ip']).version == int(sys.argv[2])]
if len(values) != 1 or values[0] not in ipaddress.ip_network(sys.argv[3]):
    raise SystemExit('unexpected pod address for firewall probe')
print(values[0])
PYTHON
    )
    # Count this candidate's probe without deciding its fate; the CIDR-wide
    # rule below remains the only rejecting rule under test.
    docker exec "$node" "$tool" -I CODEX-EGRESS 1 -s "$source_ip" -d "$target" -p tcp --dport 5555 -m comment --comment "codex-account-$family-$expected_uid"
    docker exec "$node" "$tool" -I CODEX-EGRESS 1 -s "$source_ip" -d "$target" -p tcp --dport 5556 -m comment --comment "codex-proxy-$family-$expected_uid"
    if [ "$family" = 6 ]; then
      # Nonterminating per-pod accounting distinguishes this replacement's NS
      # from other pods' use of the shared gateway exception.
      docker exec "$node" "$tool" -I CODEX-EGRESS 1 -s "$source_ip" -d "$gateway_multicast/128" -p ipv6-icmp --icmpv6-type 135 -m hl --hl-eq 255 -m comment --comment "codex-ns-flow-$expected_uid"
    fi
    echo "$phase IPv$family allowed GET"
    if [ "$family" = 4 ]; then loopback=127.0.0.1; else loopback='[::1]'; fi
    if ! docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$loopback:5556/regions"; then
      echo "$phase IPv$family fake proxy listener is unavailable" >&2
      docker inspect --format 'proxy status={{.State.Status}} exit={{.State.ExitCode}} oom={{.State.OOMKilled}}' boundary-proxy || true
      docker logs boundary-proxy 2>&1 || true
      exit 1
    fi
    if [ "$family" = 6 ]; then
      echo "$phase IPv6 neighbor counters before allowed GET"
      docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk '/codex-icmpv6-/ {print "packets=" $1, $(NF-1)}'
      mapfile -t ns_before < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk '/codex-gateway-ns/ {print $1}')
      mapfile -t ns_flow_before < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-ns-flow-$expected_uid" 'index($0, marker) {print $1}')
      test "${#ns_before[@]}" -eq 1 && [[ "${ns_before[0]}" =~ ^[0-9]+$ ]]
      test "${#ns_flow_before[@]}" -eq 1 && [[ "${ns_flow_before[0]}" =~ ^[0-9]+$ ]]
    fi
    for attempt in $(seq 1 10); do
      if "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- wget -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$host:5556/regions"; then break; fi
      sleep 2
    done
    if ! "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- wget -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$host:5556/regions"; then
      echo "$phase IPv$family allowed path remained unavailable; fake-only diagnostics follow" >&2
      docker exec "$node" "$tool" -nvx -L CODEX-EGRESS || true
      docker inspect --format 'proxy status={{.State.Status}} exit={{.State.ExitCode}} oom={{.State.OOMKilled}}' boundary-proxy || true
      docker logs boundary-proxy 2>&1 || true
      "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- ip -"$family" route || true
      if [ "$family" = 6 ]; then
        echo "$phase IPv6 neighbor counters after allowed GET"
        docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk '/codex-icmpv6-/ {print "packets=" $1, $(NF-1)}' || true
        docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 2 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' 'http://[::1]:5556/regions' || echo 'IPv6 proxy loopback control failed' >&2
        docker exec "$node" ip -6 neigh show | awk '{print $NF}' | sort | uniq -c || true
      fi
      exit 1
    fi
    if [ "$family" = 6 ] && [ "$phase" = replacement ]; then
      mapfile -t ns_after < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk '/codex-gateway-ns/ {print $1}')
      mapfile -t ns_flow_after < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-ns-flow-$expected_uid" 'index($0, marker) {print $1}')
      test "${#ns_after[@]}" -eq 1 && [[ "${ns_after[0]}" =~ ^[0-9]+$ ]] && (( ns_after[0] > ns_before[0] ))
      test "${#ns_flow_after[@]}" -eq 1 && [[ "${ns_flow_after[0]}" =~ ^[0-9]+$ ]] && (( ns_flow_after[0] > ns_flow_before[0] ))
    fi
    echo "$phase IPv$family forbidden POST"
    "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- /bin/sh -c 'result=$(wget -S -O /dev/null --post-data=x --header="X-Api-Key: fixture-key" --header="X-Api-Secret-Key: fixture-secret" "http://$1:5556/regions" 2>&1); case "$result" in *"403 Forbidden"*) exit 0;; *) exit 1;; esac' -- "$host"
    mapfile -t before < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-cidr-deny-$family" 'index($0, marker) {print $1}')
    mapfile -t account_before < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-account-$family-$expected_uid" 'index($0, marker) {print $1}')
    test "${#before[@]}" -eq 1 && [[ "${before[0]}" =~ ^[0-9]+$ ]]
    test "${#account_before[@]}" -eq 1 && [[ "${account_before[0]}" =~ ^[0-9]+$ ]]
    echo "$phase IPv$family CIDR node denial"
    "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- /bin/sh -c 'if wget -q -T 3 -O /dev/null "http://$1:5555/v2/"; then exit 42; else exit 0; fi' -- "$host"
    mapfile -t after < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-cidr-deny-$family" 'index($0, marker) {print $1}')
    mapfile -t account_after < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-account-$family-$expected_uid" 'index($0, marker) {print $1}')
    test "${#after[@]}" -eq 1 && [[ "${after[0]}" =~ ^[0-9]+$ ]] && (( after[0] > before[0] ))
    test "${#account_after[@]}" -eq 1 && [[ "${account_after[0]}" =~ ^[0-9]+$ ]] && (( account_after[0] > account_before[0] ))
    # A negative control must fail the overall test if another rule, an
    # unavailable listener, or a broken probe explains the observed denial.
    echo "$phase IPv$family denial removal control"
    docker exec "$node" "$tool" -D CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
    "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- wget -q -T 3 -O /dev/null "http://$host:5555/v2/"
    docker exec "$node" "$tool" -D CODEX-EGRESS -j RETURN
    docker exec "$node" "$tool" -A CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
    docker exec "$node" "$tool" -A CODEX-EGRESS -j RETURN
    docker exec "$node" "$tool" -C CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
    echo "$phase IPv$family non-node denial"
    docker exec "$node" "$tool" -I CODEX-EGRESS 1 -s "$source_ip" -d "$external_target" -p tcp --dport 5556 -m comment --comment "codex-external-$family-$expected_uid"
    docker exec "$node" "$tool" -A CODEX-FORWARD-COUNT -s "$source_ip" -d "$external_target" -p tcp --dport 5556 -m comment --comment "codex-forward-$family-$expected_uid"
    mapfile -t ext_before < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-external-$family-$expected_uid" 'index($0, marker) {print $1}')
    mapfile -t ext_deny_before < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-cidr-deny-$family" 'index($0, marker) {print $1}')
    mapfile -t forward_before < <(docker exec "$node" "$tool" -nvx -L CODEX-FORWARD-COUNT | awk -v marker="codex-forward-$family-$expected_uid" 'index($0, marker) {print $1}')
    test "${#ext_before[@]}" -eq 1 && [[ "${ext_before[0]}" =~ ^[0-9]+$ ]]
    test "${#ext_deny_before[@]}" -eq 1 && [[ "${ext_deny_before[0]}" =~ ^[0-9]+$ ]]
    test "${#forward_before[@]}" -eq 1 && [[ "${forward_before[0]}" =~ ^[0-9]+$ ]]
    "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- /bin/sh -c 'if wget -q -T 3 -O /dev/null --header="X-Api-Key: fixture-key" --header="X-Api-Secret-Key: fixture-secret" "http://$1:5556/regions"; then exit 42; else exit 0; fi' -- "$external_host"
    mapfile -t ext_after < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-external-$family-$expected_uid" 'index($0, marker) {print $1}')
    mapfile -t ext_deny_after < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-cidr-deny-$family" 'index($0, marker) {print $1}')
    mapfile -t forward_after < <(docker exec "$node" "$tool" -nvx -L CODEX-FORWARD-COUNT | awk -v marker="codex-forward-$family-$expected_uid" 'index($0, marker) {print $1}')
    test "${#ext_after[@]}" -eq 1 && [[ "${ext_after[0]}" =~ ^[0-9]+$ ]] && (( ext_after[0] > ext_before[0] ))
    test "${#ext_deny_after[@]}" -eq 1 && [[ "${ext_deny_after[0]}" =~ ^[0-9]+$ ]] && (( ext_deny_after[0] > ext_deny_before[0] ))
    test "${#forward_after[@]}" -eq 1 && [[ "${forward_after[0]}" =~ ^[0-9]+$ ]] && (( forward_after[0] > forward_before[0] ))
    docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$external_host:5556/regions"
    docker exec "$node" "$tool" -D CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
    "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- wget -q -T 3 -O /dev/null --header='X-Api-Key: fixture-key' --header='X-Api-Secret-Key: fixture-secret' "http://$external_host:5556/regions"
    docker exec "$node" "$tool" -D CODEX-EGRESS -j RETURN
    docker exec "$node" "$tool" -A CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
    docker exec "$node" "$tool" -A CODEX-EGRESS -j RETURN
    docker exec "$node" "$tool" -C CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
    if [ "$family" = 6 ]; then
      echo "$phase IPv6 non-NS echo denial"
      docker exec "$node" "$tool" -I CODEX-EGRESS 1 -s "$source_ip" -d "$target" -p ipv6-icmp --icmpv6-type 128 -m comment --comment "codex-echo-$expected_uid"
      mapfile -t echo_before < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-echo-$expected_uid" 'index($0, marker) {print $1}')
      mapfile -t deny_before6 < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-cidr-deny-$family" 'index($0, marker) {print $1}')
      test "${#echo_before[@]}" -eq 1 && [[ "${echo_before[0]}" =~ ^[0-9]+$ ]]
      test "${#deny_before6[@]}" -eq 1 && [[ "${deny_before6[0]}" =~ ^[0-9]+$ ]]
      "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- /bin/sh -c 'if ping -6 -c 1 -W 3 "$1" >/dev/null 2>&1; then exit 42; else exit 0; fi' -- "$target"
      mapfile -t echo_after < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-echo-$expected_uid" 'index($0, marker) {print $1}')
      mapfile -t deny_after6 < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-cidr-deny-$family" 'index($0, marker) {print $1}')
      test "${#echo_after[@]}" -eq 1 && [[ "${echo_after[0]}" =~ ^[0-9]+$ ]] && (( echo_after[0] > echo_before[0] ))
      test "${#deny_after6[@]}" -eq 1 && [[ "${deny_after6[0]}" =~ ^[0-9]+$ ]] && (( deny_after6[0] > deny_before6[0] ))
      docker exec "$node" "$tool" -D CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
      "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- ping -6 -c 1 -W 3 "$target" >/dev/null
      docker exec "$node" "$tool" -D CODEX-EGRESS -j RETURN
      docker exec "$node" "$tool" -A CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
      docker exec "$node" "$tool" -A CODEX-EGRESS -j RETURN
      docker exec "$node" "$tool" -C CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
    fi
    if [ "$family" = 4 ]; then
      # Kubelet's node-local package mirror must not become an egress relay.
      echo "$phase IPv4 registry denial"
      docker exec "$node" "$tool" -I CODEX-EGRESS 1 -s "$source_ip" -d "$target" -p tcp --dport 5000 -m comment --comment "codex-registry-$expected_uid"
      mapfile -t registry_before < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-registry-$expected_uid" 'index($0, marker) {print $1}')
      mapfile -t deny_before < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-cidr-deny-$family" 'index($0, marker) {print $1}')
      test "${#registry_before[@]}" -eq 1 && [[ "${registry_before[0]}" =~ ^[0-9]+$ ]]
      test "${#deny_before[@]}" -eq 1 && [[ "${deny_before[0]}" =~ ^[0-9]+$ ]]
      "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- /bin/sh -c 'if wget -q -T 3 -O /dev/null "http://$1:5000/v2/"; then exit 42; else exit 0; fi' -- "$host"
      mapfile -t registry_after < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-registry-$expected_uid" 'index($0, marker) {print $1}')
      mapfile -t deny_after < <(docker exec "$node" "$tool" -nvx -L CODEX-EGRESS | awk -v marker="codex-cidr-deny-$family" 'index($0, marker) {print $1}')
      test "${#registry_after[@]}" -eq 1 && [[ "${registry_after[0]}" =~ ^[0-9]+$ ]] && (( registry_after[0] > registry_before[0] ))
      test "${#deny_after[@]}" -eq 1 && [[ "${deny_after[0]}" =~ ^[0-9]+$ ]] && (( deny_after[0] > deny_before[0] ))
      docker run --rm --network "container:$node" --entrypoint wget "$image" -q -T 2 -O /dev/null http://127.0.0.1:5000/v2/
      echo "$phase IPv4 registry removal control"
      docker exec "$node" "$tool" -D CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
      "$kubectl" -n upbound-system exec "$candidate" -c boundary-probe -- wget -q -T 3 -O /dev/null "http://$host:5000/v2/"
      docker exec "$node" "$tool" -D CODEX-EGRESS -j RETURN
      docker exec "$node" "$tool" -A CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
      docker exec "$node" "$tool" -A CODEX-EGRESS -j RETURN
      docker exec "$node" "$tool" -C CODEX-EGRESS -s "$cidr" -m comment --comment "codex-cidr-deny-$family" -j REJECT
    fi
  done
  test "$("$kubectl" -n upbound-system get pod "$candidate" -o jsonpath='{.metadata.uid}')" = "$expected_uid"
}
uid=$("$kubectl" -n upbound-system get pod "$pod" -o jsonpath='{.metadata.uid}')
probe "$pod" "$uid" original
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
if [ ! -s "$RUNNER_TEMP/replacement-name" ]; then
  "$kubectl" -n upbound-system get pods -o json | python3 -c '
import json, sys
pods = json.load(sys.stdin)["items"]
for pod in pods:
    if not pod["metadata"]["name"].startswith("provider-rediscloud-"):
        continue
    statuses = pod.get("status", {}).get("containerStatuses", [])
    print({"phase": pod.get("status", {}).get("phase"), "node": pod.get("spec", {}).get("nodeName"), "podIPCount": len(pod.get("status", {}).get("podIPs", [])), "containers": [{"name": c["name"], "ready": c.get("ready"), "waitingReason": c.get("state", {}).get("waiting", {}).get("reason"), "terminatedReason": c.get("state", {}).get("terminated", {}).get("reason")} for c in statuses]})
'
  echo "replacement provider did not become ready with guarded dual-stack identity" >&2
  exit 1
fi
replacement=$(cat "$RUNNER_TEMP/replacement-name")
new_uid=$("$kubectl" -n upbound-system get pod "$replacement" -o jsonpath='{.metadata.uid}')
test "$new_uid" != "$uid"
probe "$replacement" "$new_uid" replacement
echo 'Credential-free IPv4/IPv6 node proxy and node-denial probes passed for the original and replacement provider pods. This does not establish full API/RBAC or live QA safety.'

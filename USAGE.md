# Use the Redis Cloud Crossplane provider

This walkthrough creates a Redis Cloud Pro subscription, a database, and an ACL
rule through Kubernetes resources. It includes AWS and GCP examples and deletes
the resources in dependency order. **Kind hosts Crossplane and its CRDs; Redis
Cloud hosts the subscription and database.** Creating a CR can create billable
Cloud infrastructure.

Use a non-production Redis Cloud account for this walkthrough. You need an
enabled Cloud API key pair, an account payment method, and access to the account's
API endpoint. Keep credentials and account-specific manifests outside Git.

## 1. Install Crossplane and the source-built provider

For this repository's QA verification, run Kind in GitHub Actions, not on a
developer laptop. The commands below describe the runner-side setup and resource
lifecycle. `local-deploy` means local to that runner. The existing
[Nightly QA workflow](.github/workflows/qa-live.yaml) builds the checked-out
provider and exercises AWS Pro plus Essentials using the protected
`UPTEST_CLOUD_CREDENTIALS` repository secret:

```bash
gh workflow run qa-live.yaml --repo RedisLabs/crossplane-provider-rediscloud --ref main
gh run list --repo RedisLabs/crossplane-provider-rediscloud --workflow qa-live.yaml --limit 5
```

Record the selected run's `headSha` as the tested commit. The standard workflow
does not run the GCP Pro example or pause for UI screenshots. Those require a
dedicated remote workflow that keeps the same runner and credentials available
through capture and cleanup. Do not start a second live run while the first
still owns resources.

The repository does not currently publish an installable release package. Build
the checked-out source; do not use an invented registry tag. Requirements are
Docker, Make, Python 3, `kubectl`, and Go 1.26 (the CI line). The Makefile installs
its pinned Kind, Helm, and Crossplane CLI tools. Clone with submodules:

```bash
git clone --recurse-submodules https://github.com/RedisLabs/crossplane-provider-rediscloud.git
cd crossplane-provider-rediscloud
git rev-parse HEAD

# Keep the kubeconfig separate from your normal clusters.
export KUBECONFIG="$HOME/.kube/rediscloud-example"
make local-deploy KIND_CLUSTER_NAME=rediscloud-example PLATFORMS=linux_amd64
export KUBE_CONTEXT=kind-rediscloud-example

kubectl --context "$KUBE_CONTEXT" get providers.pkg.crossplane.io
kubectl --context "$KUBE_CONTEXT" wait provider.pkg.crossplane.io/provider-rediscloud \
  --for=condition=Healthy --timeout=5m
```

Use `PLATFORMS=linux_arm64` on an ARM host. The target installs Crossplane and the
provider in `upbound-system`. A Git worktree's `.git` pointer may be unavailable
inside the build container. In that case retry with
`GO_BUILDFLAGS=-buildvcs=false`, retaining the `git rev-parse HEAD` result as the
source identity. This disables build-time VCS stamping, not provider behavior.

See [TESTING.md](TESTING.md) for development and test commands. The local-deploy
target is specifically a Kind workflow and changes the context in the selected
kubeconfig. These instructions do not install into an existing remote cluster.

## 2. Configure credentials

Create a private JSON file outside the repository, for example
`$HOME/.config/rediscloud/example-credentials.json`, with these keys:

```json
{
  "api_key": "REPLACE_WITH_ACCOUNT_API_KEY",
  "secret_key": "REPLACE_WITH_USER_API_SECRET_KEY",
  "url": "https://REPLACE_WITH_YOUR_NON_PRODUCTION_CAPI_HOST/v1"
}
```

The provider expects `secret_key`, not `api_secret_key`. `url` overrides the
Cloud API endpoint; omitting it selects the default production Redis Cloud API.
For QA, supply and verify the explicit QA URL before applying any managed CR.
Use the API host, not the browser console's host. Protect the file and load it
without putting secret values in command arguments or shell history:

```bash
export CREDS_FILE="$HOME/.config/rediscloud/example-credentials.json"
chmod 600 "$CREDS_FILE"
kubectl --context "$KUBE_CONTEXT" create secret generic rediscloud-creds \
  --namespace upbound-system --from-file=credentials="$CREDS_FILE"
kubectl --context "$KUBE_CONTEXT" apply -f - <<'YAML'
apiVersion: rediscloud.redis.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      namespace: upbound-system
      name: rediscloud-creds
      key: credentials
YAML
```

`ProviderConfig` has usage information; it is not a credential-validation probe.
A successful lookup below verifies that the credentials can reach the API.
Never publish `kubectl get secret -o yaml`, decoded credentials, Terraform state,
database passwords, or unreviewed debug logs.

## 3. Choose AWS or GCP and look up account values

Run one cloud example at a time and complete cleanup before the next.

| Example | `CLOUD` | `REGION` | Example private CIDR |
| --- | --- | --- | --- |
| AWS Pro | `AWS` | `us-east-1` | `10.88.10.0/24` |
| GCP Pro | `GCP` | `us-central1` | `10.88.11.0/24` |

Choose an unused CIDR that does not overlap your account's existing networks.
Region availability, account entitlements, and allowed capacity still depend on
the Cloud account; schema-valid YAML alone does not prove API acceptance.

```bash
export CLOUD=AWS REGION=us-east-1 DEPLOYMENT_CIDR=10.88.10.0/24
# For the GCP run instead:
# export CLOUD=GCP REGION=us-central1 DEPLOYMENT_CIDR=10.88.11.0/24
export RUN_NAME="xp-usage-$(date -u +%Y%m%d%H%M%S)"

kubectl --context "$KUBE_CONTEXT" apply -f - <<YAML
apiVersion: lookup.redis.io/v1alpha1
kind: PaymentMethod
metadata:
  name: ${RUN_NAME}-payment
spec:
  providerConfigRef:
    name: default
  forProvider:
    excludeExpired: true
---
apiVersion: lookup.redis.io/v1alpha1
kind: Regions
metadata:
  name: ${RUN_NAME}-regions
spec:
  providerConfigRef:
    name: default
  forProvider:
    providerName: ${CLOUD}
    name: ${REGION}
YAML

kubectl --context "$KUBE_CONTEXT" wait \
  paymentmethod.lookup.redis.io/"${RUN_NAME}-payment" \
  regions.lookup.redis.io/"${RUN_NAME}-regions" \
  --for=condition=Ready --timeout=5m
export PAYMENT_METHOD_ID="$(kubectl --context "$KUBE_CONTEXT" get \
  paymentmethod.lookup.redis.io "${RUN_NAME}-payment" -o jsonpath='{.status.atProvider.id}')"
```

If multiple payment methods match, add `cardType` and `lastFourNumbers` filters
from [the lookup example](examples/lookup/paymentmethod.yaml). The controller
requires exactly one match. Do not commit or include the payment ID in evidence.

## 4. Create the ACL rule and Pro subscription

The following works for either cloud selected above. The ACL rule is independent;
it defines a permission rule but does not grant a database user access by itself.
That requires an ACL role and user, covered by the existing live acceptance suite.

```bash
kubectl --context "$KUBE_CONTEXT" apply -f - <<YAML
apiVersion: acl.redis.io/v1beta1
kind: Rule
metadata:
  name: ${RUN_NAME}
spec:
  forProvider:
    name: ${RUN_NAME}
    rule: '+@read ~*'
  providerConfigRef:
    name: default
---
apiVersion: rediscloud.redis.io/v1beta1
kind: Subscription
metadata:
  name: ${RUN_NAME}
spec:
  deletionPolicy: Delete
  forProvider:
    name: ${RUN_NAME}
    memoryStorage: ram
    paymentMethodId: '${PAYMENT_METHOD_ID}'
    cloudProvider:
      - provider: ${CLOUD}
        region:
          - region: ${REGION}
            networkingDeploymentCidr: ${DEPLOYMENT_CIDR}
            multipleAvailabilityZones: true
    creationPlan:
      - quantity: 1
        replication: true
        datasetSizeInGb: 1
        throughputMeasurementBy: operations-per-second
        throughputMeasurementValue: 1000
  providerConfigRef:
    name: default
YAML

kubectl --context "$KUBE_CONTEXT" wait subscription.rediscloud.redis.io/"$RUN_NAME" \
  rule.acl.redis.io/"$RUN_NAME" --for=condition=Ready --timeout=30m
kubectl --context "$KUBE_CONTEXT" wait subscription.rediscloud.redis.io/"$RUN_NAME" \
  rule.acl.redis.io/"$RUN_NAME" --for=condition=Synced --timeout=5m
export SUBSCRIPTION_ID="$(kubectl --context "$KUBE_CONTEXT" get \
  subscription.rediscloud.redis.io "$RUN_NAME" -o jsonpath='{.status.atProvider.id}')"
```

## 5. Create the database inside that subscription

The API Kind is **`Database` in `subscription.redis.io`**, rather than a Kind
named `SubscriptionDatabase`. Its `subscriptionId` is numeric. It does not support
`subscriptionIdRef`; copy the ID only after the subscription is ready.

```bash
kubectl --context "$KUBE_CONTEXT" apply -f - <<YAML
apiVersion: subscription.redis.io/v1beta1
kind: Database
metadata:
  name: ${RUN_NAME}
spec:
  deletionPolicy: Delete
  forProvider:
    subscriptionId: ${SUBSCRIPTION_ID}
    name: ${RUN_NAME}-db
    protocol: redis
    memoryLimitInGb: 0.1
    dataPersistence: none
    replication: true
    throughputMeasurementBy: operations-per-second
    throughputMeasurementValue: 1000
  writeConnectionSecretToRef:
    name: ${RUN_NAME}-connection
    namespace: default
  providerConfigRef:
    name: default
YAML

kubectl --context "$KUBE_CONTEXT" wait database.subscription.redis.io/"$RUN_NAME" \
  --for=condition=Ready --timeout=30m
kubectl --context "$KUBE_CONTEXT" wait database.subscription.redis.io/"$RUN_NAME" \
  --for=condition=Synced --timeout=5m
```

`dataPersistence: none` is a disposable test configuration. Choose persistence,
capacity, network access, and authentication deliberately for an application.
Omitting `enablePasswordless` does not enable passwordless access. User-supplied
passwords use `passwordSecretRef`, not plaintext YAML.

## 6. Check status and connection details

```bash
kubectl --context "$KUBE_CONTEXT" get subscriptions.rediscloud.redis.io,databases.subscription.redis.io,rules.acl.redis.io
kubectl --context "$KUBE_CONTEXT" get database.subscription.redis.io "$RUN_NAME" \
  -o jsonpath='{.status.conditions}'
kubectl --context "$KUBE_CONTEXT" get database.subscription.redis.io "$RUN_NAME" \
  -o jsonpath='{.status.atProvider.id}{"\n"}'
# Print only connection Secret key names, never values:
kubectl --context "$KUBE_CONTEXT" get secret "${RUN_NAME}-connection" -n default \
  -o go-template='{{range $key, $value := .data}}{{$key}}{{"\n"}}{{end}}'
```

The connection Secret contains `public_endpoint` and `private_endpoint`. `port`
is present only when Terraform returned it. This provider does not promise a
password in that Secret. Applications can consume the endpoint keys using a
Kubernetes `secretKeyRef`; do not paste Secret values into tickets or screenshots.

Check the same subscription and database in your QA Cloud console. The database's
Terraform identifier may be `subscriptionID/databaseID`; compare the final segment
with the Cloud database ID and also verify its parent subscription. Require both
Ready and Synced, plus Cloud `active`, before recording acceptance. Capture any
active-state screenshots before deleting resources.

## 7. Delete resources and verify cleanup

Keep Crossplane, the provider, ProviderConfig, and credentials running until
managed resource deletion finishes. Delete children before parents:

```bash
kubectl --context "$KUBE_CONTEXT" delete database.subscription.redis.io "$RUN_NAME" --timeout=15m
kubectl --context "$KUBE_CONTEXT" delete subscription.rediscloud.redis.io "$RUN_NAME" --timeout=15m
kubectl --context "$KUBE_CONTEXT" delete rule.acl.redis.io "$RUN_NAME" --timeout=5m
kubectl --context "$KUBE_CONTEXT" get subscriptions.rediscloud.redis.io,databases.subscription.redis.io,rules.acl.redis.io
```

Verify through the Cloud API that `GET /subscriptions/{subscriptionID}` and
`GET /subscriptions/{subscriptionID}/databases/{databaseID}` return HTTP 404,
and that no subscription or ACL rule with the run name remains. A Kubernetes
delete request alone is not proof of Cloud deletion. If a finalizer is stuck,
inspect sanitized conditions and provider logs, restore missing credentials, and
retry normal deletion. Do not remove finalizers to make a failed cleanup appear
successful.

Only after Cloud deletion and CR absence are confirmed:

```bash
kubectl --context "$KUBE_CONTEXT" delete paymentmethod.lookup.redis.io "${RUN_NAME}-payment"
kubectl --context "$KUBE_CONTEXT" delete regions.lookup.redis.io "${RUN_NAME}-regions"
kubectl --context "$KUBE_CONTEXT" delete secret "${RUN_NAME}-connection" -n default --ignore-not-found
# After finishing all examples that use this ProviderConfig:
kubectl --context "$KUBE_CONTEXT" delete providerconfig.rediscloud.redis.io default
kubectl --context "$KUBE_CONTEXT" delete secret rediscloud-creds -n upbound-system
make controlplane.down KIND_CLUSTER_NAME=rediscloud-example
```

## Troubleshooting and further examples

| Symptom | Check |
| --- | --- |
| Provider not Healthy | Provider/ProviderRevision conditions, runtime image, and pod events. |
| Lookup not Ready | Explicit API URL, key permissions, and filters matching exactly one result. |
| Subscription create rejected | Payment method, unused CIDR, supported region, dataset size, and throughput combination. |
| Database not reconciled | Numeric parent ID, parent active status, Ready and Synced condition reasons. |
| Kind pods cannot reach Kubernetes | `kube-proxy` logs; an exhausted host inotify limit can prevent startup. |
| Delete waits on finalizer | Keep credentials and controller running; verify child deletion before parent deletion. |

- [Testing and acceptance](TESTING.md) and [suite coverage and gaps](docs/design/test-suites.md).
- [Essentials plan lookup](examples/lookup/essentialsplan.yaml): Essentials uses a
  plan ID and is a different tier from Pro; GCP Essentials does not test GCP Pro.
- [Other manifests](examples): Active-Active, private connectivity, and transit
  gateway examples require their own account/network prerequisites.
- [Glossary](GLOSSARY.md) and [Terraform alignment](docs/terraform-alignment.md).

These examples demonstrate create/read/delete. They do not establish update,
import, networking, Active-Active, or Agent Memory acceptance coverage.
---

# Managed resource examples

Each managed resource on `main` has one synthetic example below. The fields come from `examples/` and `examples-generated/`. Numbers, names, and network ranges are placeholders. Copy a real id from the object's `status.atProvider` after it exists. Payment method, Essentials plan, and region lookups are in `examples/lookup/`.

Every example uses the `default` ProviderConfig from the [README](README.md).

## ACL

### `rediscloud_acl_rule`

```yaml
apiVersion: acl.redis.io/v1beta1
kind: Rule
metadata:
  name: example-acl-rule
spec:
  forProvider:
    name: read-only-rule
    rule: "+@read ~*"
  providerConfigRef:
    name: default
```

### `rediscloud_acl_role`

```yaml
apiVersion: acl.redis.io/v1beta1
kind: Role
metadata:
  name: example-acl-role
spec:
  forProvider:
    name: example-role
    rule:
    - name: read-only-rule
      database:
      - database: 1001
        subscription: 1001
  providerConfigRef:
    name: default
```

`database` and `subscription` are numbers. Copy them from the database and subscription status.

### `rediscloud_acl_user`

```yaml
apiVersion: acl.redis.io/v1beta1
kind: User
metadata:
  name: example-acl-user
spec:
  forProvider:
    name: example-user
    role: example-role
    passwordSecretRef:
      name: example-user-password
      namespace: default
      key: password
  providerConfigRef:
    name: default
```

## Pro subscription

### `rediscloud_subscription`

```yaml
apiVersion: rediscloud.redis.io/v1beta1
kind: Subscription
metadata:
  name: example-subscription
spec:
  forProvider:
    name: example-subscription
    memoryStorage: ram
    paymentMethod: credit-card
    paymentMethodId: example-payment-method-id
    cloudProvider:
    - provider: AWS
      region:
      - region: eu-west-1
        networkingDeploymentCidr: 10.0.0.0/24
        multipleAvailabilityZones: true
        preferredAvailabilityZones:
        - euw1-az1
        - euw1-az2
        - euw1-az3
      resourceTags:
        environment: example
        team: example
    creationPlan:
    - quantity: 1
      replication: true
      datasetSizeInGb: 1
      throughputMeasurementBy: operations-per-second
      throughputMeasurementValue: 1000
    maintenanceWindows:
    - mode: manual
      window:
      - days:
        - Tuesday
        - Friday
        durationInHours: 8
        startHour: 22
  providerConfigRef:
    name: default
```

`paymentMethodId` is the string from a `PaymentMethod` lookup.

### `rediscloud_subscription_database`

```yaml
apiVersion: subscription.redis.io/v1beta1
kind: Database
metadata:
  name: example-database
spec:
  forProvider:
    # subscriptionId is a number. Copy status.atProvider.id from the subscription.
    subscriptionId: 1001
    name: example-database
    protocol: redis
    datasetSizeInGb: 1
    dataPersistence: aof-every-1-second
    throughputMeasurementBy: operations-per-second
    throughputMeasurementValue: 1000
    replication: true
    modules:
    - name: RedisJSON
    alert:
    - name: dataset-size
      value: 80
    tags:
      environment: example
  providerConfigRef:
    name: default
```

### `rediscloud_subscription_peering`

```yaml
apiVersion: subscription.redis.io/v1beta1
kind: Peering
metadata:
  name: example-peering
spec:
  forProvider:
    subscriptionIdRef:
      name: example-subscription
    providerName: AWS
    region: us-east-1
    awsAccountId: example-aws-account
    vpcId: example-vpc
    vpcCidr: 10.0.0.0/16
  providerConfigRef:
    name: default
```

## Essentials

### `rediscloud_essentials_subscription`

```yaml
apiVersion: essentials.redis.io/v1beta1
kind: Subscription
metadata:
  name: example-essentials-subscription
spec:
  forProvider:
    name: example-essentials-subscription
    # planId and paymentMethodId are numbers. Copy them from lookup status.
    planId: 1001
    paymentMethodId: 1001
  providerConfigRef:
    name: default
```

### `rediscloud_essentials_database`

```yaml
apiVersion: essentials.redis.io/v1beta1
kind: Database
metadata:
  name: example-essentials-database
spec:
  forProvider:
    name: example-essentials-database
    # subscriptionId is a number. Copy status.atProvider.id from the subscription.
    subscriptionId: 1001
    dataPersistence: none
    replication: false
    enableDefaultUser: true
    passwordSecretRef:
      name: example-database-password
      namespace: default
      key: password
    alert:
    - name: throughput-higher-than
      value: 80
    tags:
      environment: example
  providerConfigRef:
    name: default
```

## Cloud account

### `rediscloud_cloud_account`

```yaml
apiVersion: cloud.redis.io/v1beta1
kind: Account
metadata:
  name: example-cloud-account
spec:
  forProvider:
    name: example-cloud-account
    providerType: AWS
    accessKeyId: example-access-key-id
    accessSecretKeySecretRef:
      name: example-cloud-credentials
      namespace: default
      key: secret-access-key
    consoleUsername: example-console-user
    consolePasswordSecretRef:
      name: example-cloud-credentials
      namespace: default
      key: console-password
    signInLoginUrl: https://example.invalid/sign-in
  providerConfigRef:
    name: default
```

## Agent Memory

### `rediscloud_agent_memory`

```yaml
apiVersion: agent.redis.io/v1beta1
kind: Memory
metadata:
  name: example-memory
spec:
  forProvider:
    name: example-memory
    # databaseId is a number. Copy it from the database status.
    databaseId: 1001
  providerConfigRef:
    name: default
```

### `rediscloud_agent_memory_api_key`

```yaml
apiVersion: agent.redis.io/v1beta1
kind: MemoryAPIKey
metadata:
  name: example-memory-api-key
spec:
  forProvider:
    name: example-application
    storeId: example-memory-id
  providerConfigRef:
    name: default
```

`storeId` is the id of the Memory object.

## Private connectivity

### `rediscloud_private_link`

```yaml
apiVersion: private.redis.io/v1beta1
kind: Link
metadata:
  name: example-private-link
spec:
  forProvider:
    subscriptionIdRef:
      name: example-subscription
    shareName: example-private-link
    principal:
    - principal: example-principal-1
      principalAlias: example-principal-1
      principalType: aws_account
    - principal: example-principal-2
      principalAlias: example-principal-2
      principalType: aws_account
  providerConfigRef:
    name: default
```

### `rediscloud_private_service_connect`

```yaml
apiVersion: private.redis.io/v1beta1
kind: ServiceConnect
metadata:
  name: example-service-connect
spec:
  forProvider:
    subscriptionIdRef:
      name: example-subscription
  providerConfigRef:
    name: default
```

### `rediscloud_private_service_connect_endpoint`

```yaml
apiVersion: private.redis.io/v1beta1
kind: ServiceConnectEndpoint
metadata:
  name: example-service-connect-endpoint
spec:
  forProvider:
    subscriptionIdRef:
      name: example-subscription
    privateServiceConnectServiceId: 1001
    endpointConnectionName: example-endpoint
    gcpProjectId: example-gcp-project
    gcpVpcName: example-vpc
    gcpVpcSubnetName: example-subnet
  providerConfigRef:
    name: default
```

`privateServiceConnectServiceId` is the number from the Service Connect status.

### `rediscloud_private_service_connect_endpoint_accepter`

```yaml
apiVersion: private.redis.io/v1beta1
kind: ServiceConnectEndpointAccepter
metadata:
  name: example-service-connect-endpoint-accepter
spec:
  forProvider:
    subscriptionIdRef:
      name: example-subscription
    privateServiceConnectServiceId: 1001
    privateServiceConnectEndpointId: 1001
    action: accept
  providerConfigRef:
    name: default
```

## Active-Active

### `rediscloud_active_active_subscription`

```yaml
apiVersion: active.redis.io/v1beta1
kind: ActiveSubscription
metadata:
  name: example-active-subscription
spec:
  forProvider:
    name: example-active-subscription
    cloudProvider: AWS
    paymentMethod: credit-card
    paymentMethodId: example-payment-method-id
    creationPlan:
    - quantity: 1
      datasetSizeInGb: 1
      region:
      - region: us-east-1
        networkingDeploymentCidr: 192.168.0.0/24
        readOperationsPerSecond: 1000
        writeOperationsPerSecond: 1000
      - region: us-east-2
        networkingDeploymentCidr: 10.0.1.0/24
        readOperationsPerSecond: 1000
        writeOperationsPerSecond: 1000
  providerConfigRef:
    name: default
```

### `rediscloud_active_active_subscription_database`

```yaml
apiVersion: active.redis.io/v1beta1
kind: ActiveSubscriptionDatabase
metadata:
  name: example-active-database
spec:
  forProvider:
    name: example-active-database
    # subscriptionId is a number. Copy status.atProvider.id from the subscription.
    subscriptionId: 1001
    datasetSizeInGb: 1
    globalDataPersistence: aof-every-1-second
    globalPasswordSecretRef:
      name: example-database-password
      namespace: default
      key: password
    globalSourceIps:
    - 192.168.0.0/16
    globalAlert:
    - name: dataset-size
      value: 80
    overrideRegion:
    - name: us-east-1
      localReadOperationsPerSecond: 2000
      localWriteOperationsPerSecond: 1000
      overrideGlobalDataPersistence: none
      overrideGlobalSourceIps:
      - 192.168.10.0/24
      overrideGlobalAlert:
      - name: dataset-size
        value: 60
      overrideGlobalPasswordSecretRef:
        name: example-database-password
        namespace: default
        key: password
    tags:
      environment: example
  providerConfigRef:
    name: default
```

### `rediscloud_active_active_subscription_regions`

The regions example and the database overrideRegion throughput are alternatives, and they are not applied to the same database.

```yaml
apiVersion: active.redis.io/v1beta1
kind: ActiveSubscriptionRegions
metadata:
  name: example-active-regions
spec:
  forProvider:
    subscriptionIdRef:
      name: example-active-subscription
    deleteRegions: false
    region:
    - region: us-east-1
      networkingDeploymentCidr: 192.168.0.0/24
      database:
      - databaseName: example-active-regions-database
        databaseId: 1002
        localReadOperationsPerSecond: 1000
        localWriteOperationsPerSecond: 1000
    - region: us-east-2
      networkingDeploymentCidr: 10.0.1.0/24
      database:
      - databaseName: example-active-regions-database
        databaseId: 1002
        localReadOperationsPerSecond: 1000
        localWriteOperationsPerSecond: 1000
  providerConfigRef:
    name: default
```

### `rediscloud_active_active_subscription_peering`

```yaml
apiVersion: active.redis.io/v1beta1
kind: ActiveSubscriptionPeering
metadata:
  name: example-active-peering
spec:
  forProvider:
    subscriptionIdRef:
      name: example-active-subscription
    sourceRegion: us-east-1
    destinationRegion: eu-west-2
    awsAccountId: example-aws-account
    vpcId: example-vpc
    vpcCidr: 10.0.10.0/24
  providerConfigRef:
    name: default
```

### `rediscloud_active_active_private_link`

```yaml
apiVersion: active.redis.io/v1beta1
kind: ActivePrivateLink
metadata:
  name: example-active-private-link
spec:
  forProvider:
    subscriptionIdRef:
      name: example-active-subscription
    regionId: 1
    shareName: example-active-private-link
    principal:
    - principal: example-principal-1
      principalAlias: example-principal-1
      principalType: aws_account
    - principal: example-principal-2
      principalAlias: example-principal-2
      principalType: aws_account
  providerConfigRef:
    name: default
```

`regionId` is the number from an Active-Active regions lookup.

### `rediscloud_active_active_private_service_connect`

```yaml
apiVersion: active.redis.io/v1beta1
kind: ActivePrivateServiceConnect
metadata:
  name: example-active-service-connect
spec:
  forProvider:
    subscriptionIdRef:
      name: example-active-subscription
    regionId: 1
  providerConfigRef:
    name: default
```

### `rediscloud_active_active_private_service_connect_endpoint`

```yaml
apiVersion: active.redis.io/v1beta1
kind: ActivePrivateServiceConnectEndpoint
metadata:
  name: example-active-service-connect-endpoint
spec:
  forProvider:
    subscriptionIdRef:
      name: example-active-subscription
    regionId: 1
    privateServiceConnectServiceId: 1001
    endpointConnectionName: example-endpoint
    gcpProjectId: example-gcp-project
    gcpVpcName: example-vpc
    gcpVpcSubnetName: example-subnet
  providerConfigRef:
    name: default
```

### `rediscloud_active_active_private_service_connect_endpoint_accepter`

```yaml
apiVersion: active.redis.io/v1beta1
kind: ActivePrivateServiceConnectEndpointAccepter
metadata:
  name: example-active-service-connect-endpoint-accepter
spec:
  forProvider:
    subscriptionIdRef:
      name: example-active-subscription
    regionId: 1
    privateServiceConnectServiceId: 1001
    privateServiceConnectEndpointId: 1001
    action: accept
  providerConfigRef:
    name: default
```

## Transit Gateway

### `rediscloud_transit_gateway_attachment`

```yaml
apiVersion: transit.redis.io/v1beta1
kind: GatewayAttachment
metadata:
  name: example-transit-gateway-attachment
spec:
  forProvider:
    subscriptionIdRef:
      name: example-subscription
    tgwId: 1001
  providerConfigRef:
    name: default
```

### `rediscloud_transit_gateway_route`

```yaml
apiVersion: transit.redis.io/v1beta1
kind: GatewayRoute
metadata:
  name: example-transit-gateway-route
spec:
  forProvider:
    subscriptionIdRef:
      name: example-subscription
    tgwId: 1001
    cidrs:
    - 10.10.20.0/24
    - 10.10.30.0/24
  providerConfigRef:
    name: default
```

### `rediscloud_transit_gateway_invitation_acceptor`

```yaml
apiVersion: transit.redis.io/v1beta1
kind: GatewayInvitationAcceptor
metadata:
  name: example-transit-gateway-invitation-acceptor
spec:
  forProvider:
    subscriptionIdRef:
      name: example-subscription
    tgwInvitationId: 1001
    action: accept
  providerConfigRef:
    name: default
```

### `rediscloud_active_active_transit_gateway_attachment`

```yaml
apiVersion: active.redis.io/v1beta1
kind: ActiveTransitGatewayAttachment
metadata:
  name: example-active-transit-gateway-attachment
spec:
  forProvider:
    subscriptionIdRef:
      name: example-active-subscription
    regionId: example-region-id
    tgwId: 1001
  providerConfigRef:
    name: default
```

### `rediscloud_active_active_transit_gateway_route`

```yaml
apiVersion: active.redis.io/v1beta1
kind: ActiveTransitGatewayRoute
metadata:
  name: example-active-transit-gateway-route
spec:
  forProvider:
    subscriptionIdRef:
      name: example-active-subscription
    regionId: example-region-id
    tgwId: 1001
    cidrs:
    - 10.10.20.0/24
    - 10.10.30.0/24
  providerConfigRef:
    name: default
```

### `rediscloud_active_active_transit_gateway_invitation_acceptor`

```yaml
apiVersion: active.redis.io/v1beta1
kind: ActiveTransitGatewayInvitationAcceptor
metadata:
  name: example-active-transit-gateway-invitation-acceptor
spec:
  forProvider:
    subscriptionIdRef:
      name: example-active-subscription
    regionId: 1
    tgwInvitationId: 1001
    action: accept
  providerConfigRef:
    name: default
```

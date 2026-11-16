# Usage

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
    memoryLimitInGb: 1
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
    memoryLimitInGb: 1
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
      - databaseName: example-active-database
        databaseId: 1001
        localReadOperationsPerSecond: 1000
        localWriteOperationsPerSecond: 1000
    - region: us-east-2
      networkingDeploymentCidr: 10.0.1.0/24
      database:
      - databaseName: example-active-database
        databaseId: 1001
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

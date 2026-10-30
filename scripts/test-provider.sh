#!/usr/bin/env bash
set -euo pipefail

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Logging functions
log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

log_success() {
    echo -e "${GREEN}[SUCCESS]${NC} $1"
}

log_warning() {
    echo -e "${YELLOW}[WARNING]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Configuration
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-rediscloud-test}"
CROSSPLANE_NAMESPACE="${CROSSPLANE_NAMESPACE:-crossplane-system}"
PROVIDER_NAMESPACE="${PROVIDER_NAMESPACE:-crossplane-system}"
KUBECONFIG_PATH="${KUBECONFIG_PATH:-${PROJECT_DIR}/.kube/config}"

# Create .kube directory if it doesn't exist
mkdir -p "$(dirname "$KUBECONFIG_PATH")"
export KUBECONFIG="$KUBECONFIG_PATH"

# Configure for podman
export KIND_EXPERIMENTAL_PROVIDER=podman
export DOCKER_HOST=${DOCKER_HOST:-unix:///run/user/$(id -u)/podman/podman.sock}

# Cleanup function
cleanup() {
    if [[ "${1:-}" != "keep" ]]; then
        log_info "Cleaning up..."
        kind delete cluster --name "$KIND_CLUSTER_NAME" 2>/dev/null || true
        log_success "Cleanup completed"
    fi
}

# Trap cleanup on exit
trap cleanup EXIT

# Function to wait for pods to be ready
wait_for_pods() {
    local namespace="$1"
    local app_label="$2"
    local timeout="${3:-300}"

    log_info "Waiting for pods with label app=$app_label in namespace $namespace to be ready..."
    kubectl wait --for=condition=ready pod -l "app=$app_label" -n "$namespace" --timeout="${timeout}s" || {
        log_error "Pods failed to become ready within ${timeout}s"
        kubectl get pods -n "$namespace" -l "app=$app_label"
        kubectl describe pods -n "$namespace" -l "app=$app_label"
        return 1
    }
    log_success "Pods are ready"
}

# Function to check if credentials are set
check_credentials() {
    if [[ "${REDISCLOUD_API_KEY:-}" == "your-api-key-here" || -z "${REDISCLOUD_API_KEY:-}" ]]; then
        log_warning "RedisCloud API key not set. Update REDISCLOUD_API_KEY in .envrc"
        log_warning "Get credentials from: RedisCloud Console → Account Settings → API Keys"
        return 1
    fi

    if [[ "${REDISCLOUD_SECRET_KEY:-}" == "your-secret-key-here" || -z "${REDISCLOUD_SECRET_KEY:-}" ]]; then
        log_warning "RedisCloud secret key not set. Update REDISCLOUD_SECRET_KEY in .envrc"
        return 1
    fi

    log_success "RedisCloud credentials are configured"
    return 0
}

# Function to create kind cluster
create_kind_cluster() {
    log_info "Creating kind cluster: $KIND_CLUSTER_NAME"
    
    # Set KIND_EXPERIMENTAL_PROVIDER to use podman
    export KIND_EXPERIMENTAL_PROVIDER=podman

    if kind get clusters | grep -q "^$KIND_CLUSTER_NAME$"; then
        log_info "Kind cluster $KIND_CLUSTER_NAME already exists"
        kind export kubeconfig --name "$KIND_CLUSTER_NAME" --kubeconfig "$KUBECONFIG_PATH"
    else
        # Create kind cluster with registry support
        cat <<EOF | kind create cluster --name "$KIND_CLUSTER_NAME" --config=-
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 80
    hostPort: 8080
    protocol: TCP
  - containerPort: 443
    hostPort: 8443
    protocol: TCP
EOF
        kind export kubeconfig --name "$KIND_CLUSTER_NAME" --kubeconfig "$KUBECONFIG_PATH"
    fi

    log_success "Kind cluster created and kubeconfig exported to $KUBECONFIG_PATH"
}

# Function to install Crossplane
install_crossplane() {
    log_info "Installing Crossplane..."

    # Add Crossplane Helm repository
    helm repo add crossplane-stable https://charts.crossplane.io/stable
    helm repo update

    # Install Crossplane
    helm upgrade --install crossplane crossplane-stable/crossplane \
        --namespace "$CROSSPLANE_NAMESPACE" \
        --create-namespace \
        --wait \
        --timeout=300s

    log_success "Crossplane installed successfully"

    # Wait for Crossplane to be ready
    wait_for_pods "$CROSSPLANE_NAMESPACE" "crossplane"

    log_success "Crossplane is ready"
}

# Function to load provider image into kind
load_provider_image() {
    log_info "Loading provider image into kind cluster..."

    # Build the provider if not already built
    if [[ ! -f "${PROJECT_DIR}/_output/xpkg/linux_amd64/provider-rediscloud-"*.xpkg ]]; then
        log_info "Building provider first..."
        cd "$PROJECT_DIR"
        make build
    fi

    # Get the xpkg file
    local xpkg_file=$(ls "${PROJECT_DIR}/_output/xpkg/linux_amd64/provider-rediscloud-"*.xpkg 2>/dev/null | head -1)
    if [[ -z "$xpkg_file" ]]; then
        log_error "No xpkg file found"
        return 1
    fi
    
    # Import xpkg as OCI image
    local image_tag="build-fbf0549c/provider-rediscloud-amd64:latest"
    log_info "Importing xpkg as OCI image..."
    
    # Use podman to import the xpkg
    podman load -i "$xpkg_file" || {
        # If that doesn't work, try copying the xpkg as-is
        log_info "Trying alternative load method..."
        # The xpkg is already an OCI image, so we can import it
        podman import "$xpkg_file" "$image_tag"
    }
    
    # Load image into kind
    kind load docker-image "$image_tag" --name "$KIND_CLUSTER_NAME"

    log_success "Provider image loaded into kind cluster"
}

# Function to install provider
install_provider() {
    log_info "Installing RedisCloud provider..."

    # Create provider installation
    cat <<EOF | kubectl apply -f -
apiVersion: pkg.crossplane.io/v1
kind: Provider
metadata:
  name: provider-rediscloud
spec:
  package: build-fbf0549c/provider-rediscloud-amd64:latest
  packagePullPolicy: Never
EOF

    log_info "Waiting for provider to be installed and healthy..."
    kubectl wait --for=condition=installed provider provider-rediscloud --timeout=300s
    kubectl wait --for=condition=healthy provider provider-rediscloud --timeout=300s

    log_success "Provider installed and healthy"
}

# Function to create credentials secret
create_credentials() {
    if ! check_credentials; then
        log_error "Credentials not properly configured. Please update .envrc and run: direnv allow"
        return 1
    fi

    log_info "Creating credentials secret..."

    # Create namespace if it doesn't exist
    kubectl create namespace "$PROVIDER_NAMESPACE" --dry-run=client -o yaml | kubectl apply -f -

    # Create secret with credentials
    kubectl create secret generic rediscloud-creds \
        --namespace="$PROVIDER_NAMESPACE" \
        --from-literal=credentials="{\"api_key\":\"$REDISCLOUD_API_KEY\",\"secret_key\":\"$REDISCLOUD_SECRET_KEY\",\"url\":\"${REDISCLOUD_URL:-https://api.redislabs.com/v1}\"}" \
        --dry-run=client -o yaml | kubectl apply -f -

    log_success "Credentials secret created"
}

# Function to create ProviderConfig
create_provider_config() {
    log_info "Creating ProviderConfig..."

    cat <<EOF | kubectl apply -f -
apiVersion: rediscloud.redis.io/v1beta1
kind: ProviderConfig
metadata:
  name: default
spec:
  credentials:
    source: Secret
    secretRef:
      name: rediscloud-creds
      namespace: $PROVIDER_NAMESPACE
      key: credentials
EOF

    log_success "ProviderConfig created"
}

# Function to test provider with a sample resource
test_provider() {
    log_info "Testing provider with a sample Essentials subscription..."

    # Create a test essentials subscription (free tier)
    cat <<EOF | kubectl apply -f -
apiVersion: essentials.redis.io/v1beta1
kind: Subscription
metadata:
  name: test-subscription
spec:
  forProvider:
    name: "crossplane-test-subscription"
    planId: 1  # Free plan ID
  providerConfigRef:
    name: default
EOF

    log_info "Waiting for subscription to be ready..."
    kubectl wait --for=condition=ready subscription test-subscription --timeout=600s || {
        log_error "Subscription failed to become ready"
        kubectl describe subscription test-subscription
        kubectl get events --sort-by=.metadata.creationTimestamp
        return 1
    }

    log_success "Test subscription created successfully!"

    # Show the created resource
    kubectl get subscription test-subscription -o yaml
}

# Function to show cluster status
show_status() {
    log_info "Cluster Status:"
    echo
    log_info "Crossplane Pods:"
    kubectl get pods -n "$CROSSPLANE_NAMESPACE"
    echo
    log_info "Provider Status:"
    kubectl get providers
    echo
    log_info "ProviderConfig:"
    kubectl get providerconfig
    echo
    log_info "RedisCloud Resources:"
    kubectl get subscription.essentials.redis.io 2>/dev/null || echo "No Essentials subscriptions found"
    kubectl get subscription.rediscloud.redis.io 2>/dev/null || echo "No Pro subscriptions found"
}

# Function to cleanup test resources
cleanup_test_resources() {
    log_info "Cleaning up test resources..."
    kubectl delete subscription test-subscription --ignore-not-found=true
    log_success "Test resources cleaned up"
}

# Main function
main() {
    local action="${1:-all}"

    case "$action" in
        "cluster")
            create_kind_cluster
            ;;
        "crossplane")
            install_crossplane
            ;;
        "provider")
            load_provider_image
            install_provider
            ;;
        "config")
            create_credentials
            create_provider_config
            ;;
        "test")
            test_provider
            ;;
        "status")
            show_status
            ;;
        "cleanup")
            cleanup_test_resources
            ;;
        "all")
            create_kind_cluster
            install_crossplane
            load_provider_image
            install_provider
            create_credentials
            create_provider_config
            show_status
            log_success "Setup complete! Run '$0 test' to test the provider"
            cleanup keep  # Don't cleanup on successful completion
            ;;
        *)
            echo "Usage: $0 [cluster|crossplane|provider|config|test|status|cleanup|all]"
            echo
            echo "Commands:"
            echo "  cluster    - Create kind cluster"
            echo "  crossplane - Install Crossplane"
            echo "  provider   - Load and install RedisCloud provider"
            echo "  config     - Create credentials and ProviderConfig"
            echo "  test       - Test provider with sample resource"
            echo "  status     - Show cluster and provider status"
            echo "  cleanup    - Clean up test resources"
            echo "  all        - Run complete setup (default)"
            echo
            echo "Environment variables:"
            echo "  REDISCLOUD_API_KEY    - Your RedisCloud API key"
            echo "  REDISCLOUD_SECRET_KEY - Your RedisCloud secret key"
            echo "  KIND_CLUSTER_NAME     - Kind cluster name (default: rediscloud-test)"
            exit 1
            ;;
    esac
}

# Run main function
main "$@"
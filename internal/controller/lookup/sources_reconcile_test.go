// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	"github.com/RedisLabs/provider-rediscloud/apis/v1beta1"
)

func TestCloudAccountReconcileFiltersWithoutExcludingInternalByDefault(t *testing.T) {
	var calls []string
	kube := lookupClient(t, routes(t, &calls, map[string]string{
		"/cloud-accounts": `{"cloudAccounts":[{"id":1,"name":"internal","provider":"AWS","accessKeyId":"internal-key"},{"id":2,"name":"example-account","provider":"GCP","accessKeyId":"example-key"}]}`,
	}), &lookupv1alpha1.CloudAccount{
		ObjectMeta: metav1.ObjectMeta{Name: "example-cloud-account"},
		Spec: lookupv1alpha1.CloudAccountSpec{
			ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig},
			ForProvider: lookupv1alpha1.CloudAccountParameters{
				ExcludeInternalAccount: boolPtr(false),
				Name:                   stringPtr("internal"),
			},
		},
	})
	if err := reconcileReady(t, (&CloudAccountReconciler{Client: kube}).Reconcile, "example-cloud-account"); err != nil {
		t.Fatal(err)
	}
	got := &lookupv1alpha1.CloudAccount{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "example-cloud-account"}, got); err != nil {
		t.Fatal(err)
	}
	if diff := cmp.Diff(lookupv1alpha1.CloudAccountObservation{
		ID: "1", Name: "internal", AccessKeyID: "internal-key", ProviderType: "AWS",
	}, got.Status.AtProvider); diff != "" {
		t.Fatalf("status mismatch (-want +got):\n%s", diff)
	}
	if strings.Join(calls, " ") != "GET /cloud-accounts" {
		t.Fatalf("calls = %v", calls)
	}
}

func TestDataPersistenceReconcileAllowsEmptyCatalog(t *testing.T) {
	var calls []string
	kube := lookupClient(t, routes(t, &calls, map[string]string{
		"/data-persistence": `{"dataPersistence":[]}`,
	}), &lookupv1alpha1.DataPersistence{
		ObjectMeta: metav1.ObjectMeta{Name: "example-data-persistence"},
		Spec:       lookupv1alpha1.DataPersistenceSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}},
	})
	if err := reconcileReady(t, (&DataPersistenceReconciler{Client: kube}).Reconcile, "example-data-persistence"); err != nil {
		t.Fatal(err)
	}
	got := &lookupv1alpha1.DataPersistence{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "example-data-persistence"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.AtProvider.ID != "ALL" || got.Status.AtProvider.Options != nil {
		t.Fatalf("status = %#v", got.Status.AtProvider)
	}
}

func TestEssentialsSubscriptionReconcileWritesZeroPaymentMethod(t *testing.T) {
	var calls []string
	kube := lookupClient(t, routes(t, &calls, map[string]string{
		"/fixed/subscriptions": `{"subscriptions":[{"id":1001,"name":"example-essentials","status":"active","planId":7,"creationDate":"2026-01-02T03:04:05Z"}]}`,
	}), &lookupv1alpha1.EssentialsSubscription{
		ObjectMeta: metav1.ObjectMeta{Name: "example-essentials"},
		Spec: lookupv1alpha1.EssentialsSubscriptionSpec{
			ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig},
			ForProvider:       lookupv1alpha1.EssentialsSubscriptionParameters{Name: stringPtr("example-essentials")},
		},
	})
	if err := reconcileReady(t, (&EssentialsSubscriptionReconciler{Client: kube}).Reconcile, "example-essentials"); err != nil {
		t.Fatal(err)
	}
	got := &lookupv1alpha1.EssentialsSubscription{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "example-essentials"}, got); err != nil {
		t.Fatal(err)
	}
	created := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC).String()
	if diff := cmp.Diff(lookupv1alpha1.EssentialsSubscriptionObservation{
		ID: "1001", Name: "example-essentials", Status: "active", PlanID: "7", PaymentMethodID: "0", CreationDate: created,
	}, got.Status.AtProvider); diff != "" {
		t.Fatalf("status mismatch (-want +got):\n%s", diff)
	}
}

func TestPrivateLinkEndpointScriptReconcileChecksSubscriptionAndLink(t *testing.T) {
	var calls []string
	kube := lookupClient(t, taskRoutes(t, &calls, map[string]taskBody{
		"/subscriptions/1001":                              {direct: `{"id":1001,"name":"example-pro"}`},
		"/subscriptions/1001/private-link":                 {task: "link", resource: `{"status":"active","shareName":"example-share"}`},
		"/subscriptions/1001/private-link/endpoint-script": {task: "script", resource: `{"resourceEndpointScript":"echo example"}`},
	}), &lookupv1alpha1.PrivateLinkEndpointScript{
		ObjectMeta: metav1.ObjectMeta{Name: "example-script"},
		Spec: lookupv1alpha1.PrivateLinkEndpointScriptSpec{
			ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig},
			ForProvider:       lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "1001"},
		},
	})
	if err := reconcileReady(t, (&PrivateLinkEndpointScriptReconciler{Client: kube}).Reconcile, "example-script"); err != nil {
		t.Fatal(err)
	}
	got := &lookupv1alpha1.PrivateLinkEndpointScript{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "example-script"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.AtProvider.ID != "1001" || got.Status.AtProvider.EndpointScript != "echo example" {
		t.Fatalf("status = %#v", got.Status.AtProvider)
	}
	joined := strings.Join(calls, "\n")
	if !strings.Contains(joined, "GET /subscriptions/1001\n") || !strings.Contains(joined, "private-link/endpoint-script") {
		t.Fatalf("calls =\n%s", joined)
	}
	if !strings.Contains(joined, "includeTerraformAwsScript=true") {
		t.Fatalf("script call dropped the Terraform query:\n%s", joined)
	}
}

func TestPrivateServiceConnectEndpointsReconcileRequestsGCPScript(t *testing.T) {
	var calls []string
	kube := lookupClient(t, taskRoutes(t, &calls, map[string]taskBody{
		"/subscriptions/1001/private-service-connect/9": {
			task:     "endpoints",
			resource: `{"pscServiceId":9,"endpoints":[{"id":3,"status":"active","gcpProjectId":"example-gcp-project","gcpVpcName":"example-vpc","gcpVpcSubnetName":"example-subnet","endpointConnectionName":"example-connection"},{"id":4,"status":"rejected"}]}`,
		},
		"/subscriptions/1001/private-service-connect/9/endpoints/3/creationScripts": {
			task:     "script",
			resource: `{"script":{"terraformGcp":{"serviceAttachments":[{"name":"example-attachment","dnsRecord":"example.invalid","ipAddressName":"example-ip","forwardingRuleName":"example-rule"}]}}}`,
		},
	}), &lookupv1alpha1.PrivateServiceConnectEndpoints{
		ObjectMeta: metav1.ObjectMeta{Name: "example-psc-endpoints"},
		Spec: lookupv1alpha1.PrivateServiceConnectEndpointsSpec{
			ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig},
			ForProvider: lookupv1alpha1.PrivateServiceConnectEndpointsParameters{
				SubscriptionID: "1001", PrivateServiceConnectServiceID: "9",
			},
		},
	})
	if err := reconcileReady(t, (&PrivateServiceConnectEndpointsReconciler{Client: kube}).Reconcile, "example-psc-endpoints"); err != nil {
		t.Fatal(err)
	}
	got := &lookupv1alpha1.PrivateServiceConnectEndpoints{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "example-psc-endpoints"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.AtProvider.ID != "1001/9" || len(got.Status.AtProvider.Endpoints) != 2 {
		t.Fatalf("status = %#v", got.Status.AtProvider)
	}
	if len(got.Status.AtProvider.Endpoints[0].ServiceAttachments) != 1 || got.Status.AtProvider.Endpoints[1].ServiceAttachments != nil {
		t.Fatalf("attachments = %#v", got.Status.AtProvider.Endpoints)
	}
	joined := strings.Join(calls, "\n")
	if strings.Count(joined, "creationScripts") != 1 {
		t.Fatalf("creation script calls =\n%s", joined)
	}
	if !strings.Contains(joined, "includeTerraformGcpScript=true") {
		t.Fatalf("script query missing:\n%s", joined)
	}
	if strings.Contains(joined, "/endpoints/4/") {
		t.Fatalf("rejected endpoint requested a script:\n%s", joined)
	}
}

func TestTransitGatewayReconcileDoesNotSendTimeout(t *testing.T) {
	var calls []string
	kube := lookupClient(t, taskRoutes(t, &calls, map[string]taskBody{
		"/subscriptions/1001/transitGateways": {
			task:     "tgw",
			resource: `{"tgws":[{"id":4,"awsTgwUid":"example-tgw","attachmentUid":"example-attachment","status":"active","attachmentStatus":"available","awsAccountId":"example-aws-account","cidrs":[{"cidrAddress":"10.0.0.0/16"}]}]}`,
		},
	}), &lookupv1alpha1.TransitGateway{
		ObjectMeta: metav1.ObjectMeta{Name: "example-transit-gateway"},
		Spec: lookupv1alpha1.TransitGatewaySpec{
			ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig},
			ForProvider: lookupv1alpha1.TransitGatewayParameters{
				SubscriptionID: "1001", AWSTGWUID: stringPtr("example-tgw"),
			},
		},
	})
	if err := reconcileReady(t, (&TransitGatewayReconciler{Client: kube}).Reconcile, "example-transit-gateway"); err != nil {
		t.Fatal(err)
	}
	got := &lookupv1alpha1.TransitGateway{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "example-transit-gateway"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.AtProvider.ID != "1001/4" || got.Status.AtProvider.CIDRs[0] != "10.0.0.0/16" {
		t.Fatalf("status = %#v", got.Status.AtProvider)
	}
	joined := strings.Join(calls, "\n")
	if strings.Contains(joined, "timeout") || strings.Contains(joined, "wait") {
		t.Fatalf("timeout was sent:\n%s", joined)
	}
}

func TestTransitGatewayInvitationsReconcileAllowsEmptyList(t *testing.T) {
	var calls []string
	kube := lookupClient(t, taskRoutes(t, &calls, map[string]taskBody{
		"/subscriptions/1001/transitGateways/invitations": {task: "invite", resource: `{"resources":[]}`},
	}), &lookupv1alpha1.TransitGatewayInvitations{
		ObjectMeta: metav1.ObjectMeta{Name: "example-invitations"},
		Spec: lookupv1alpha1.TransitGatewayInvitationsSpec{
			ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig},
			ForProvider:       lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "1001"},
		},
	})
	if err := reconcileReady(t, (&TransitGatewayInvitationsReconciler{Client: kube}).Reconcile, "example-invitations"); err != nil {
		t.Fatal(err)
	}
	got := &lookupv1alpha1.TransitGatewayInvitations{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "example-invitations"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.AtProvider.ID != "1001" || got.Status.AtProvider.Invitations != nil {
		t.Fatalf("status = %#v", got.Status.AtProvider)
	}
}

func TestActiveActiveRegionsReconcileRejectsEmptyRegionList(t *testing.T) {
	var calls []string
	kube := lookupClient(t, routes(t, &calls, map[string]string{
		"/subscriptions":              `{"subscriptions":[{"id":1001,"name":"example-aa","deploymentType":"active-active"},{"id":1002,"name":"example-aa","deploymentType":"single-region"}]}`,
		"/subscriptions/1001/regions": `{"subscriptionId":1001,"regions":[]}`,
	}), &lookupv1alpha1.ActiveActiveSubscriptionRegions{
		ObjectMeta: metav1.ObjectMeta{Name: "example-aa-regions"},
		Spec: lookupv1alpha1.ActiveActiveSubscriptionRegionsSpec{
			ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig},
			ForProvider:       lookupv1alpha1.ActiveActiveSubscriptionRegionsParameters{SubscriptionName: "example-aa"},
		},
	})
	result, err := (&ActiveActiveSubscriptionRegionsReconciler{Client: kube}).Reconcile(context.Background(), request("example-aa-regions"))
	if err != nil {
		t.Fatal(err)
	}
	if result.RequeueAfter != requeueFailed {
		t.Fatalf("requeue = %s", result.RequeueAfter)
	}
	got := &lookupv1alpha1.ActiveActiveSubscriptionRegions{}
	if err := kube.Get(context.Background(), types.NamespacedName{Name: "example-aa-regions"}, got); err != nil {
		t.Fatal(err)
	}
	if got.Status.GetCondition(xpv1.TypeReady).Status != corev1.ConditionFalse || got.Status.AtProvider.ID != "" {
		t.Fatalf("status = %#v", got.Status)
	}
}

func TestLookupDeleteDoesNotCallAPI(t *testing.T) {
	now := metav1.Now()
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("delete called %s", r.URL.Path)
	}), &lookupv1alpha1.ACLRole{
		ObjectMeta: metav1.ObjectMeta{Name: "example-role", DeletionTimestamp: &now, Finalizers: []string{"test"}},
		Spec: lookupv1alpha1.ACLRoleSpec{
			ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig},
			ForProvider:       lookupv1alpha1.ACLRoleParameters{Name: "example-role"},
		},
	})
	result, err := (&ACLRoleReconciler{Client: kube}).Reconcile(context.Background(), request("example-role"))
	if err != nil || result.RequeueAfter != 0 {
		t.Fatalf("delete result = %#v %v", result, err)
	}
}

type taskBody struct {
	direct   string
	task     string
	resource string
}

func routes(t *testing.T, calls *[]string, bodies map[string]string) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.RequestURI())
		body, ok := bodies[r.URL.Path]
		if !ok {
			t.Errorf("unexpected %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	})
}

func taskRoutes(t *testing.T, calls *[]string, bodies map[string]taskBody) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.RequestURI())
		w.Header().Set("Content-Type", "application/json")
		if strings.HasPrefix(r.URL.Path, "/tasks/") {
			id := strings.TrimPrefix(r.URL.Path, "/tasks/")
			for key := range bodies {
				if bodies[key].task == id {
					_, _ = io.WriteString(w, `{"taskId":"`+id+`","status":"processing-completed","response":{"resource":`+bodies[key].resource+`}}`)
					return
				}
			}
			t.Errorf("unknown task %s", id)
			http.NotFound(w, r)
			return
		}
		path := r.URL.Path
		if i := strings.Index(path, "?"); i >= 0 {
			path = path[:i]
		}
		body, ok := bodies[path]
		if !ok {
			t.Errorf("unexpected %s %s", r.Method, r.URL.RequestURI())
			http.NotFound(w, r)
			return
		}
		if body.direct != "" {
			_, _ = io.WriteString(w, body.direct)
			return
		}
		_, _ = io.WriteString(w, `{"taskId":"`+body.task+`"}`)
	})
}

func lookupClient(t *testing.T, handler http.Handler, objects ...client.Object) client.Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	scheme := runtime.NewScheme()
	for _, add := range []func(*runtime.Scheme) error{
		corev1.AddToScheme,
		v1beta1.SchemeBuilder.AddToScheme,
		lookupv1alpha1.AddToScheme,
	} {
		if err := add(scheme); err != nil {
			t.Fatal(err)
		}
	}
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{Name: "creds", Namespace: "crossplane-system"},
		Data: map[string][]byte{
			"credentials": []byte(`{"api_key":"key","secret_key":"secret","url":"` + server.URL + `"}`),
		},
	}
	config := &v1beta1.ProviderConfig{
		ObjectMeta: metav1.ObjectMeta{Name: exampleProviderConfig},
		Spec: v1beta1.ProviderConfigSpec{
			Credentials: v1beta1.ProviderCredentials{
				Source: xpv1.CredentialsSourceSecret,
				CommonCredentialSelectors: xpv1.CommonCredentialSelectors{
					SecretRef: &xpv1.SecretKeySelector{
						SecretReference: xpv1.SecretReference{Name: "creds", Namespace: "crossplane-system"},
						Key:             "credentials",
					},
				},
			},
		},
	}
	objs := make([]client.Object, 0, 2+len(objects))
	objs = append(objs, secret, config)
	objs = append(objs, objects...)
	return fake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(objects...).WithObjects(objs...).Build()
}

func reconcileReady(t *testing.T, reconcileFn func(context.Context, reconcile.Request) (reconcile.Result, error), name string) error {
	t.Helper()
	result, err := reconcileFn(context.Background(), request(name))
	if err != nil {
		return err
	}
	if result.RequeueAfter != requeue {
		t.Fatalf("requeue = %s", result.RequeueAfter)
	}
	return nil
}

func boolPtr(value bool) *bool {
	return &value
}

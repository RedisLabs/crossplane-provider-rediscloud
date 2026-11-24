package qatfplan

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	kexec "k8s.io/utils/exec"

	api "github.com/RedisLabs/provider-rediscloud/apis/essentials/v1beta1"
	providerconfig "github.com/RedisLabs/provider-rediscloud/config"
	xpmeta "github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpfake "github.com/crossplane/crossplane-runtime/v2/pkg/resource/fake"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	ujconfig "github.com/crossplane/upjet/v2/pkg/config"
	ujcontroller "github.com/crossplane/upjet/v2/pkg/controller"
	ujresource "github.com/crossplane/upjet/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	cfake "sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type fixtureStore struct{ ws *terraform.Workspace }

func (s fixtureStore) Workspace(context.Context, ujresource.SecretClient, ujresource.Terraformed, terraform.Setup, *ujconfig.Resource) (*terraform.Workspace, error) {
	return s.ws, nil
}

func verifyManagedNoop(t *testing.T, ws *terraform.Workspace, spy *spyExecutor, callback *fakeCallback) *managed.Reconciler {
	t.Helper()
	ctx := context.Background()
	scheme := runtime.NewScheme()
	if err := api.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	item := &api.Database{ObjectMeta: metav1.ObjectMeta{Name: fixtureName, UID: types.UID("fixture-uid"), Annotations: map[string]string{xpmeta.AnnotationKeyExternalName: "1/2"}}}
	name, eviction, persistence, replication, sub := fixtureName, "allkeys-lru", "none", false, float64(1)
	item.Spec.ForProvider.Name = &name
	item.Spec.ForProvider.DataEviction = &eviction
	item.Spec.ForProvider.DataPersistence = &persistence
	item.Spec.ForProvider.Replication = &replication
	item.Spec.ForProvider.SubscriptionID = &sub
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionAll})
	item.SetConditions(xpv2.Available())
	kube := cfake.NewClientBuilder().WithScheme(scheme).WithStatusSubresource(item).WithObjects(item).Build()
	mgr := &xpfake.Manager{Client: kube, Scheme: scheme}
	setup := func(context.Context, client.Client, xpresource.Managed) (terraform.Setup, error) {
		return terraform.Setup{}, nil
	}
	connector := ujcontroller.NewConnector(kube, fixtureStore{ws}, setup, providerconfig.GetProvider().Resources["rediscloud_essentials_database"], ujcontroller.WithCallbackProvider(callback))
	rec := managed.NewReconciler(mgr, xpresource.ManagedKind(api.Database_GroupVersionKind), managed.WithExternalConnector(connector))
	before := len(spy.snapshot())
	for i := 0; i < 6; i++ {
		if _, err := rec.Reconcile(ctx, reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}}); err != nil {
			t.Fatalf("reconcile %d: %v", i, err)
		}
	}
	commands := spy.snapshot()[before:]
	plans := 0
	for _, cmd := range commands {
		if strings.Contains(cmd, "terraform plan ") {
			plans++
		}
		if strings.Contains(cmd, "terraform apply ") && !strings.Contains(cmd, "-refresh-only") {
			t.Fatalf("unexpected mutation apply on no-op: %s", cmd)
		}
	}
	if plans < 2 {
		t.Fatalf("managed reconciles did not reach plan twice: %v", commands)
	}
	return rec
}

type spyExecutor struct {
	kexec.Interface
	mu       sync.Mutex
	commands [][]string
}

func (s *spyExecutor) CommandContext(ctx context.Context, command string, args ...string) kexec.Cmd {
	s.mu.Lock()
	s.commands = append(s.commands, append([]string{command}, args...))
	s.mu.Unlock()
	return s.Interface.CommandContext(ctx, command, args...)
}
func (s *spyExecutor) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]string, 0, len(s.commands))
	for _, c := range s.commands {
		result = append(result, strings.Join(c, " "))
	}
	return result
}

func verifyManagedDrift(t *testing.T, rec *managed.Reconciler, spy *spyExecutor, callback *fakeCallback) {
	t.Helper()
	before := len(spy.snapshot())
	if _, err := rec.Reconcile(context.Background(), reconcile.Request{NamespacedName: types.NamespacedName{Name: fixtureName}}); err != nil {
		t.Fatalf("drift reconcile: %v", err)
	}
	select {
	case err := <-callback.done:
		if err == nil {
			t.Fatal("reject-only fake update unexpectedly succeeded")
		}
	case <-time.After(30 * time.Second):
		t.Fatal("asynchronous update did not finish")
	}
	for _, cmd := range spy.snapshot()[before:] {
		if strings.Contains(cmd, "terraform apply ") && !strings.Contains(cmd, "-refresh-only") {
			return
		}
	}
	t.Fatalf("managed reconcile did not issue an update apply: %v", spy.snapshot()[before:])
}

type fakeCallback struct{ done chan error }

func (f *fakeCallback) result() terraform.CallbackFn {
	return func(err error, _ context.Context) error { f.done <- err; return nil }
}
func (f *fakeCallback) Create(types.NamespacedName, bool) terraform.CallbackFn  { return f.result() }
func (f *fakeCallback) Update(types.NamespacedName, bool) terraform.CallbackFn  { return f.result() }
func (f *fakeCallback) Destroy(types.NamespacedName, bool) terraform.CallbackFn { return f.result() }

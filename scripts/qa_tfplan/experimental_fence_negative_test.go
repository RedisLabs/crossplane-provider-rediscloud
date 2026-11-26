// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package qatfplan

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"testing"
	"time"

	pro "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
	"github.com/RedisLabs/provider-rediscloud/internal/experimental/deletefence"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func TestExperimentalFenceCrashAndStaleCallback(t *testing.T) { //nolint:gocyclo // Check every persistence and replacement boundary.
	requireHostedFixture(t)
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Fatal("hosted API server assets required")
	}
	scheme := runtime.NewScheme()
	if err := pro.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	item := &pro.Database{ObjectMeta: metav1.ObjectMeta{Name: "fixture-crash", Finalizers: []string{managed.FinalizerName}}}
	name := fixtureProDBName
	parent := float64(1)
	item.Spec.ForProvider.Name = &name
	item.Spec.ForProvider.SubscriptionID = &parent
	item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionObserve})
	meta.SetExternalName(item, fixtureProDBID)
	kube := newProEnvtestClient(t, scheme, item)
	ctx := context.Background()
	if err := kube.Delete(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := kube.Get(ctx, client.ObjectKeyFromObject(item), item); err != nil {
		t.Fatal(err)
	}
	token, err := deletefence.Acquire(ctx, kube, kube, item)
	if err != nil {
		t.Fatal(err)
	}
	binding := experimentalBinding{snapshot: item.DeepCopy(), token: token}
	calls, deletes := 0, 0
	underlying := managed.ExternalClientFns{ObserveFn: func(context.Context, xpresource.Managed) (managed.ExternalObservation, error) {
		calls++
		return managed.ExternalObservation{ResourceExists: false}, nil
	}, DeleteFn: func(context.Context, xpresource.Managed) (managed.ExternalDelete, error) {
		deletes++
		return managed.ExternalDelete{}, nil
	}}
	guarded := &experimentalExternal{ExternalClient: underlying, kube: kube}
	if _, err := guarded.Observe(ctx, item); err == nil || calls != 0 {
		t.Fatal("crash-before-dispatch did not hold observation")
	}
	if _, err := guarded.Delete(ctx, item); err == nil || deletes != 0 {
		t.Fatal("crash-before-dispatch allowed a delete")
	}
	cb := &experimentalCallback{kube: kube, base: &fakeCallback{done: make(chan error, 1)}}
	wrong := binding
	wrong.token = "wrong"
	if err := cb.finish(ctx, client.ObjectKeyFromObject(item), wrong); err == nil {
		t.Fatal("wrong token completed intent")
	}
	old := item.DeepCopy()
	fresh := &pro.Database{}
	if err := kube.Get(ctx, client.ObjectKeyFromObject(item), fresh); err != nil {
		t.Fatal(err)
	}
	fresh.Finalizers = nil
	if err := kube.Update(ctx, fresh); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 50; i++ {
		err := kube.Get(ctx, client.ObjectKeyFromObject(old), &pro.Database{})
		if apierrors.IsNotFound(err) {
			break
		}
		if err != nil || i == 49 {
			t.Fatalf("old fixture not removed: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
	replacement := &pro.Database{ObjectMeta: metav1.ObjectMeta{Name: old.Name, Finalizers: []string{managed.FinalizerName}}, Spec: old.Spec}
	meta.SetExternalName(replacement, fixtureProDBID)
	if err := kube.Create(ctx, replacement); err != nil {
		t.Fatal(err)
	}
	if replacement.UID == old.UID {
		t.Fatal("replacement UID unchanged")
	}
	if err := cb.finish(ctx, client.ObjectKeyFromObject(replacement), binding); err == nil {
		t.Fatal("stale callback completed replacement")
	}
	if held, err := deletefence.Held(ctx, kube, old); !held || err == nil {
		t.Fatal("old UID did not fail closed")
	}
}

func TestExperimentalFenceCallbackBindings(t *testing.T) { //nolint:gocyclo // Exercise two interleaved UID-bound callbacks.
	requireHostedFixture(t)
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Fatal("hosted API server assets required")
	}
	scheme := runtime.NewScheme()
	if err := pro.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	makeItem := func(n string) *pro.Database {
		obj := &pro.Database{ObjectMeta: metav1.ObjectMeta{Name: n, Finalizers: []string{managed.FinalizerName}}}
		name := fixtureProDBName
		parent := float64(1)
		obj.Spec.ForProvider.Name = &name
		obj.Spec.ForProvider.SubscriptionID = &parent
		obj.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionObserve})
		meta.SetExternalName(obj, fixtureProDBID)
		return obj
	}
	first := makeItem("first")
	kube := newProEnvtestClient(t, scheme, first)
	second := makeItem("second")
	ctx := context.Background()
	if err := kube.Create(ctx, second); err != nil {
		t.Fatal(err)
	}
	cb := &experimentalCallback{kube: kube, base: &fakeCallback{done: make(chan error, 2)}, bindings: map[types.NamespacedName]experimentalBinding{}}
	for _, obj := range []*pro.Database{first, second} {
		if err := kube.Delete(ctx, obj); err != nil {
			t.Fatal(err)
		}
		if err := kube.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			t.Fatal(err)
		}
		token, err := deletefence.Acquire(ctx, kube, kube, obj)
		if err != nil {
			t.Fatal(err)
		}
		cb.bindings[client.ObjectKeyFromObject(obj)] = experimentalBinding{snapshot: obj.DeepCopy(), token: token}
	}
	callbackFirst := cb.Destroy(client.ObjectKeyFromObject(first), false)
	callbackSecond := cb.Destroy(client.ObjectKeyFromObject(second), false)
	if err := callbackSecond(nil, ctx); err != nil {
		t.Fatal(err)
	}
	if err := callbackFirst(nil, ctx); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := <-cb.base.done; err != nil {
			t.Fatal(err)
		}
	}
	for _, obj := range []*pro.Database{first, second} {
		if err := kube.Get(ctx, client.ObjectKeyFromObject(obj), obj); err != nil {
			t.Fatal(err)
		}
		var marker experimentalIntent
		if err := json.Unmarshal([]byte(obj.Annotations[deletefence.Annotation]), &marker); err != nil || !marker.TestCompleted || marker.UID != string(obj.UID) {
			t.Fatal("callback did not complete its own intent")
		}
	}
}

// afterReservation simulates a writer changing the CR after CAS but before
// the wrapper's follow-up read. This does not model changes after that read.
type afterReservation struct {
	client.Client
	mutation string
	reads    int
	mutated  bool
}

func (c *afterReservation) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	c.reads++
	if c.reads != 2 {
		return c.Client.Get(ctx, key, obj, opts...)
	}
	current := &pro.Database{}
	if err := c.Client.Get(ctx, key, current); err != nil {
		return err
	}
	switch c.mutation {
	case "marker":
		delete(current.Annotations, deletefence.Annotation)
	case "external":
		meta.SetExternalName(current, "1/99")
	case "spec":
		value := "allkeys-lru"
		current.Spec.ForProvider.DataEviction = &value
	case "finalizer":
		current.Finalizers = nil
	}
	if err := c.Update(ctx, current); err != nil {
		return err
	}
	c.mutated = true
	return c.Client.Get(ctx, key, obj, opts...)
}

func TestExperimentalFencePostReservationMutation(t *testing.T) { //nolint:gocyclo // Each mutation must hold before the underlying Delete.
	requireHostedFixture(t)
	if os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Fatal("hosted API server assets required")
	}
	scheme := runtime.NewScheme()
	if err := pro.AddToScheme(scheme); err != nil {
		t.Fatal(err)
	}
	makeItem := func(n string) *pro.Database {
		obj := &pro.Database{ObjectMeta: metav1.ObjectMeta{Name: n, Finalizers: []string{managed.FinalizerName}}}
		name := fixtureProDBName
		parent := float64(1)
		obj.Spec.ForProvider.Name = &name
		obj.Spec.ForProvider.SubscriptionID = &parent
		obj.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionObserve})
		meta.SetExternalName(obj, fixtureProDBID)
		return obj
	}
	first := makeItem("mutation-marker")
	kube := newProEnvtestClient(t, scheme, first)
	ctx := context.Background()
	for _, kind := range []string{"marker", "external", "spec", "finalizer"} {
		t.Run(kind, func(t *testing.T) {
			item := first
			if kind != "marker" {
				item = makeItem("mutation-" + kind)
				if err := kube.Create(ctx, item); err != nil {
					t.Fatal(err)
				}
			}
			if err := kube.Delete(ctx, item); err != nil {
				t.Fatal(err)
			}
			if err := kube.Get(ctx, client.ObjectKeyFromObject(item), item); err != nil {
				t.Fatal(err)
			}
			injected := &afterReservation{Client: kube, mutation: kind}
			deletes := 0
			underlying := managed.ExternalClientFns{DeleteFn: func(context.Context, xpresource.Managed) (managed.ExternalDelete, error) {
				deletes++
				return managed.ExternalDelete{}, nil
			}}
			guarded := &experimentalExternal{ExternalClient: underlying, kube: injected, callback: &experimentalCallback{kube: injected}}
			if _, err := guarded.Delete(ctx, item); err == nil || deletes != 0 || !injected.mutated || injected.reads != 2 {
				t.Fatal("post-reservation mutation allowed underlying delete")
			}
		})
	}
}

func TestExperimentalFakeAbsenceGuard(t *testing.T) {
	requireHostedFixture(t)
	var mu sync.Mutex
	status := http.StatusNotFound
	calls := 0
	targetCalls := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		targetCalls++
		w.WriteHeader(http.StatusNotFound)
	}))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if r.Method != http.MethodGet || r.URL.Path != fixtureProDatabasePath || r.Header.Get("X-Api-Key") != dummyCredential || r.Header.Get("X-Api-Secret-Key") != dummyCredential {
			t.Error("incorrect fake absence request")
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		if status == http.StatusFound {
			w.Header().Set("Location", target.URL)
		}
		w.WriteHeader(status)
	}))
	defer server.Close()
	for _, tc := range []struct {
		name   string
		status int
		ok     bool
	}{
		{"absent", http.StatusNotFound, true}, {"present", http.StatusOK, false}, {"forbidden", http.StatusForbidden, false}, {"unavailable", http.StatusServiceUnavailable, false}, {"redirect", http.StatusFound, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mu.Lock()
			status = tc.status
			before := calls
			mu.Unlock()
			err := verifyExperimentalFakeAbsence(context.Background(), server.URL)
			mu.Lock()
			defer mu.Unlock()
			if (err == nil) != tc.ok || calls != before+1 {
				t.Fatalf("unexpected fake absence result: err=%v calls=%d", err, calls-before)
			}
			if targetCalls != 0 {
				t.Fatal("redirect target was contacted")
			}
		})
	}
	for _, raw := range []string{"https://127.0.0.1:1", "http://localhost:1", "http://127.0.0.1:1/path", "http://127.0.0.1:1/?a=b", server.URL + "?", server.URL + "#"} {
		mu.Lock()
		before := calls
		mu.Unlock()
		err := verifyExperimentalFakeAbsence(context.Background(), raw)
		mu.Lock()
		after := calls
		mu.Unlock()
		if err == nil || after != before {
			t.Fatal("unsafe fixture origin accepted")
		}
	}
}

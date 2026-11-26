// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package deletefence

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	dbv1 "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	xpv2 "github.com/crossplane/crossplane/apis/v2/core/v2"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
)

type committedErrorClient struct{ client.Client }

func (c committedErrorClient) Update(ctx context.Context, obj client.Object, opts ...client.UpdateOption) error {
	if err := c.Client.Update(ctx, obj, opts...); err != nil {
		return err
	}
	return errors.New("injected response loss after API-server commit")
}

func TestDeleteFenceAPIServer(t *testing.T) { //nolint:gocyclo // Each isolated API-server scenario is explicit.
	if os.Getenv("GITHUB_ACTIONS") != "true" || os.Getenv("KUBEBUILDER_ASSETS") == "" {
		t.Skip("hosted API-server assets required")
	}
	crd, err := os.ReadFile(filepath.Join("..", "..", "..", "package", "crds", "subscription.redis.io_databases.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if writeErr := os.WriteFile(filepath.Join(dir, "database.yaml"), crd, 0o600); writeErr != nil {
		t.Fatal(writeErr)
	}
	environment := &envtest.Environment{CRDDirectoryPaths: []string{dir}, ErrorIfCRDPathMissing: true}
	cfg, err := environment.Start()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if stopErr := environment.Stop(); stopErr != nil {
			t.Errorf("stop API server: %v", stopErr)
		}
	})
	scheme := runtime.NewScheme()
	if schemeErr := dbv1.AddToScheme(scheme); schemeErr != nil {
		t.Fatal(schemeErr)
	}
	kube, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	other, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	makeDeleting := func(name string) *dbv1.Database {
		t.Helper()
		value := "fixture"
		item := &dbv1.Database{ObjectMeta: metav1.ObjectMeta{Name: name, Finalizers: []string{"fixture.test/hold"}}}
		item.Spec.ForProvider.Name = &value
		item.SetManagementPolicies(xpv2.ManagementPolicies{xpv2.ManagementActionObserve})
		meta.SetExternalName(item, "1/42")
		if err := kube.Create(ctx, item); err != nil {
			t.Fatal(err)
		}
		if err := kube.Delete(ctx, item); err != nil {
			t.Fatal(err)
		}
		if err := kube.Get(ctx, types.NamespacedName{Name: name}, item); err != nil {
			t.Fatal(err)
		}
		if item.UID == "" || item.ResourceVersion == "" || item.DeletionTimestamp.IsZero() {
			t.Fatal("API server did not persist deleting identity")
		}
		return item
	}
	t.Run("absent_marker", func(t *testing.T) {
		item := makeDeleting("absent")
		if held, holdErr := Held(ctx, kube, item); held || holdErr != nil {
			t.Fatal("absent marker unexpectedly held")
		}
	})
	t.Run("competing_acquire", func(t *testing.T) {
		item := makeDeleting("race")
		start := make(chan struct{})
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, c := range []client.Client{kube, other} {
			wg.Add(1)
			go func(c client.Client) {
				defer wg.Done()
				<-start
				token, acquireErr := Acquire(ctx, c, c, item.DeepCopy())
				if acquireErr == nil && token == "" {
					acquireErr = errors.New("empty token")
				}
				results <- acquireErr
			}(c)
		}
		close(start)
		wg.Wait()
		close(results)
		success, held := 0, 0
		for acquireErr := range results {
			switch {
			case acquireErr == nil:
				success++
			case errors.Is(acquireErr, ErrHold):
				held++
			default:
				t.Fatal(acquireErr)
			}
		}
		if success != 1 || held != 1 {
			t.Fatalf("success=%d held=%d", success, held)
		}
	})
	t.Run("committed_response_lost", func(t *testing.T) {
		item := makeDeleting("ambiguous")
		token, acquireErr := Acquire(ctx, kube, committedErrorClient{Client: kube}, item)
		if token != "" || !errors.Is(acquireErr, ErrHold) {
			t.Fatal("ambiguous API update authorized dispatch")
		}
		if token, acquireErr = Acquire(ctx, other, other, item); token != "" || !errors.Is(acquireErr, ErrHold) {
			t.Fatal("committed intent was reacquired")
		}
	})
	t.Run("empty_marker", func(t *testing.T) {
		item := makeDeleting("empty")
		annotations := item.GetAnnotations()
		annotations[Annotation] = ""
		item.SetAnnotations(annotations)
		if err := kube.Update(ctx, item); err != nil {
			t.Fatal(err)
		}
		if held, holdErr := Held(ctx, kube, item); !held || holdErr != nil {
			t.Fatal("empty marker did not hold")
		}
		if token, acquireErr := Acquire(ctx, kube, kube, item); token != "" || !errors.Is(acquireErr, ErrHold) {
			t.Fatal("empty marker authorized dispatch")
		}
	})
	t.Run("replaced_uid", func(t *testing.T) {
		item := makeDeleting("replace")
		old := item.DeepCopy()
		item.Finalizers = nil
		if err := kube.Update(ctx, item); err != nil {
			t.Fatal(err)
		}
		for i := 0; i < 50; i++ {
			getErr := kube.Get(ctx, types.NamespacedName{Name: item.Name}, &dbv1.Database{})
			if apierrors.IsNotFound(getErr) {
				break
			}
			if getErr != nil || i == 49 {
				t.Fatalf("old object removal: %v", getErr)
			}
			time.Sleep(100 * time.Millisecond)
		}
		item = makeDeleting("replace")
		if item.UID == old.UID {
			t.Fatal("replacement did not receive a new UID")
		}
		if token, acquireErr := Acquire(ctx, kube, kube, old); token != "" || !errors.Is(acquireErr, ErrHold) {
			t.Fatal("old UID authorized replacement dispatch")
		}
		if held, holdErr := Held(ctx, kube, old); !held || !errors.Is(holdErr, ErrHold) {
			t.Fatal("old UID did not fail closed")
		}
	})
}

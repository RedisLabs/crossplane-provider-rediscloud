// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package qatfplan

// Test-only experiment: not registered with a product controller. These
// annotations and fake callback are not an authentication or security boundary.
import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"slices"
	"sync"

	pro "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
	"github.com/RedisLabs/provider-rediscloud/internal/experimental/deletefence"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	xpresource "github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/upjet/v2/pkg/terraform"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var errExperimentalHold = errors.New("experimental delete intent remains uncertain")

type experimentalIntent struct {
	Version       int    `json:"version"`
	UID           string `json:"uid"`
	ExternalName  string `json:"externalName"`
	Token         string `json:"token"`
	TestCompleted bool   `json:"testCompleted,omitempty"`
}
type experimentalBinding struct {
	snapshot *pro.Database
	token    string
}

// This is deliberately constrained to the single fake fixture identity, not a
// production identity validator. API writers can still alter annotations.
func fixtureSameIdentity(current, snapshot *pro.Database) bool {
	return current != nil && snapshot != nil && current.UID == snapshot.UID && current.UID != "" && !current.GetDeletionTimestamp().IsZero() && slices.Contains(current.Finalizers, managed.FinalizerName) && meta.GetExternalName(current) == fixtureProDBID && meta.GetExternalName(snapshot) == fixtureProDBID && current.Spec.ForProvider.SubscriptionID != nil && *current.Spec.ForProvider.SubscriptionID == 1 && reflect.DeepEqual(current.Spec, snapshot.Spec)
}
func matchingIntent(current *pro.Database, b experimentalBinding, completed bool) bool {
	var data experimentalIntent
	raw, exists := current.Annotations[deletefence.Annotation]
	return exists && json.Unmarshal([]byte(raw), &data) == nil && data.Version == 1 && data.UID == string(b.snapshot.UID) && data.ExternalName == meta.GetExternalName(b.snapshot) && data.Token == b.token && b.token != "" && data.TestCompleted == completed
}

type experimentalCallback struct {
	base     *fakeCallback
	kube     client.Client
	mu       sync.Mutex
	bindings map[types.NamespacedName]experimentalBinding
}

func (c *experimentalCallback) Create(n types.NamespacedName, b bool) terraform.CallbackFn {
	return c.base.Create(n, b)
}
func (c *experimentalCallback) Update(n types.NamespacedName, b bool) terraform.CallbackFn {
	return c.base.Update(n, b)
}
func (c *experimentalCallback) Destroy(n types.NamespacedName, b bool) terraform.CallbackFn {
	c.mu.Lock()
	binding, ok := c.bindings[n]
	delete(c.bindings, n)
	c.mu.Unlock()
	return func(err error, ctx context.Context) error {
		if err == nil {
			if !ok {
				err = errExperimentalHold
			} else {
				err = c.finish(ctx, n, binding)
			}
		}
		c.base.done <- err
		return nil
	}
}
func (c *experimentalCallback) finish(ctx context.Context, n types.NamespacedName, b experimentalBinding) error {
	if b.snapshot == nil || b.token == "" {
		return errExperimentalHold
	}
	current := &pro.Database{}
	if err := c.kube.Get(ctx, n, current); err != nil || !fixtureSameIdentity(current, b.snapshot) || !matchingIntent(current, b, false) {
		return errExperimentalHold
	}
	var data experimentalIntent
	if json.Unmarshal([]byte(current.Annotations[deletefence.Annotation]), &data) != nil {
		return errExperimentalHold
	}
	data.TestCompleted = true
	encoded, err := json.Marshal(data)
	if err != nil {
		return errExperimentalHold
	}
	annotations := current.GetAnnotations()
	annotations[deletefence.Annotation] = string(encoded)
	current.SetAnnotations(annotations)
	if err := c.kube.Update(ctx, current); err != nil {
		return errExperimentalHold
	}
	return nil
}

type experimentalConnector struct {
	inner    managed.ExternalConnector
	kube     client.Client
	callback *experimentalCallback
}

func (c *experimentalConnector) Connect(ctx context.Context, mg xpresource.Managed) (managed.ExternalClient, error) {
	inner, err := c.inner.Connect(ctx, mg)
	if err != nil {
		return nil, err
	}
	return &experimentalExternal{ExternalClient: inner, kube: c.kube, callback: c.callback}, nil
}

type experimentalExternal struct {
	managed.ExternalClient
	kube     client.Client
	callback *experimentalCallback
}

func (e *experimentalExternal) Observe(ctx context.Context, mg xpresource.Managed) (managed.ExternalObservation, error) {
	snapshot, ok := mg.(*pro.Database)
	if !ok {
		return managed.ExternalObservation{}, errExperimentalHold
	}
	current := &pro.Database{}
	if err := e.kube.Get(ctx, client.ObjectKeyFromObject(snapshot), current); err != nil || current.UID != snapshot.UID {
		return managed.ExternalObservation{}, errExperimentalHold
	}
	raw, held := current.Annotations[deletefence.Annotation]
	if held {
		var data experimentalIntent
		if json.Unmarshal([]byte(raw), &data) != nil || data.Version != 1 || data.UID != string(current.UID) || data.ExternalName != meta.GetExternalName(current) || data.Token == "" || !data.TestCompleted || !fixtureSameIdentity(current, snapshot) {
			return managed.ExternalObservation{}, errExperimentalHold
		}
	}
	return e.ExternalClient.Observe(ctx, mg)
}
func (e *experimentalExternal) Delete(ctx context.Context, mg xpresource.Managed) (managed.ExternalDelete, error) {
	snapshot, ok := mg.(*pro.Database)
	if !ok || !fixtureSameIdentity(snapshot, snapshot) {
		return managed.ExternalDelete{}, errExperimentalHold
	}
	token, err := deletefence.Acquire(ctx, e.kube, e.kube, snapshot)
	if err != nil {
		return managed.ExternalDelete{}, errExperimentalHold
	}
	binding := experimentalBinding{snapshot: snapshot.DeepCopy(), token: token}
	current := &pro.Database{}
	if err := e.kube.Get(ctx, client.ObjectKeyFromObject(snapshot), current); err != nil || !fixtureSameIdentity(current, snapshot) || !matchingIntent(current, binding, false) {
		return managed.ExternalDelete{}, errExperimentalHold
	}
	snapshot.ResourceVersion = current.ResourceVersion
	e.callback.mu.Lock()
	if e.callback.bindings == nil {
		e.callback.bindings = make(map[types.NamespacedName]experimentalBinding)
	}
	e.callback.bindings[client.ObjectKeyFromObject(snapshot)] = binding
	e.callback.mu.Unlock()
	return e.ExternalClient.Delete(ctx, mg)
}

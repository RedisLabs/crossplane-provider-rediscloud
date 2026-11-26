// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

// Package deletefence is a disabled CAS experiment. It is not wired to a
// controller and its result does not authorize a Cloud write. CR annotations
// alone are not a protected trust boundary.
package deletefence

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	dbv1 "github.com/RedisLabs/provider-rediscloud/apis/subscription/v1beta1"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Annotation is an experimental intent marker; only trusted writers may edit it.
const Annotation = "rediscloud.crossplane.io/experimental-delete-intent"

var ErrHold = errors.New("delete intent exists or cannot be established; manual resolution required")

type intent struct {
	Version      int    `json:"version"`
	UID          string `json:"uid"`
	ExternalName string `json:"externalName"`
	Token        string `json:"token"`
}

// Acquire experiments with storing an intent. Its success must not be treated
// as permission to dispatch: identity validation, finalizer protection, marker
// immutability, writer fencing and safe recovery have not been established.
// The reader must be an uncached API
// reader, and the client update must use resourceVersion optimistic concurrency.
// Any update error, including an ambiguous response after commit, forbids dispatch.
// No automatic clearing or success transition is provided by this experiment.
func Acquire(ctx context.Context, reader client.Reader, writer client.Client, snapshot *dbv1.Database) (string, error) {
	if snapshot == nil || snapshot.UID == "" || snapshot.GetDeletionTimestamp().IsZero() || meta.GetExternalName(snapshot) == "" {
		return "", ErrHold
	}
	current := &dbv1.Database{}
	if err := reader.Get(ctx, types.NamespacedName{Name: snapshot.Name}, current); err != nil {
		return "", ErrHold
	}
	if current.UID != snapshot.UID || current.GetDeletionTimestamp().IsZero() || meta.GetExternalName(current) != meta.GetExternalName(snapshot) || !reflect.DeepEqual(current.Spec, snapshot.Spec) || current.ResourceVersion == "" {
		return "", ErrHold
	}
	if _, exists := current.Annotations[Annotation]; exists {
		return "", ErrHold
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", ErrHold
	}
	token := hex.EncodeToString(random[:])
	raw, err := json.Marshal(intent{Version: 1, UID: string(current.UID), ExternalName: meta.GetExternalName(current), Token: token})
	if err != nil {
		return "", ErrHold
	}
	annotations := current.GetAnnotations()
	if annotations == nil {
		annotations = map[string]string{}
	}
	annotations[Annotation] = string(raw)
	current.SetAnnotations(annotations)
	if err := writer.Update(ctx, current); err != nil {
		return "", ErrHold
	}
	if current.UID != snapshot.UID || current.GetDeletionTimestamp().IsZero() || meta.GetExternalName(current) != meta.GetExternalName(snapshot) || !reflect.DeepEqual(current.Spec, snapshot.Spec) || current.Annotations[Annotation] != string(raw) {
		return "", ErrHold
	}
	return token, nil
}

// Held treats every existing marker, including malformed or empty values, as
// uncertain. Missing object or failed API read also fails closed.
func Held(ctx context.Context, reader client.Reader, snapshot *dbv1.Database) (bool, error) {
	if snapshot == nil || snapshot.UID == "" {
		return true, ErrHold
	}
	current := &dbv1.Database{}
	if err := reader.Get(ctx, types.NamespacedName{Name: snapshot.Name}, current); err != nil || current.UID != snapshot.UID {
		return true, ErrHold
	}
	_, exists := current.Annotations[Annotation]
	return exists, nil
}

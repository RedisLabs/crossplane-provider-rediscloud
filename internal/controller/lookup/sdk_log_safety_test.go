// SPDX-FileCopyrightText: 2024 The Crossplane Authors <https://crossplane.io>
//
// SPDX-License-Identifier: Apache-2.0

package lookup

import (
	"bytes"
	"context"
	"log"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	lookupv1alpha1 "github.com/RedisLabs/provider-rediscloud/apis/lookup/v1alpha1"
	xpv1 "github.com/crossplane/crossplane/apis/v2/core/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// A PSC lookup starts an asynchronous task. A 404 while polling causes the
// SDK to retry, and its default logger prints the raw HTTP error body.
func TestPSCAsyncTaskRetryDoesNotLogResponseBody(t *testing.T) {
	const sentinel = "fixture-sensitive-task-error"
	const name = "subject"
	var logs bytes.Buffer
	previous := log.Writer()
	log.SetOutput(&logs)
	t.Cleanup(func() { log.SetOutput(previous) })
	var taskCalls atomic.Int32
	obj := &lookupv1alpha1.PrivateServiceConnect{ObjectMeta: metav1.ObjectMeta{Name: name}, Spec: lookupv1alpha1.PrivateServiceConnectSpec{ProviderConfigRef: xpv1.Reference{Name: exampleProviderConfig}, ForProvider: lookupv1alpha1.SubscriptionIDParameters{SubscriptionID: "123"}}}
	kube := lookupClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/subscriptions/123/private-service-connect":
			_, _ = w.Write([]byte(`{"taskId":"fixture-task"}`))
		case "/tasks/fixture-task":
			if taskCalls.Add(1) == 1 {
				w.WriteHeader(http.StatusNotFound)
			} else {
				w.WriteHeader(http.StatusForbidden)
			}
			_, _ = w.Write([]byte(`{"message":"` + sentinel + `"}`))
		default:
			t.Error("unexpected fake API route")
			w.WriteHeader(http.StatusInternalServerError)
		}
	}), obj)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	result, err := (&PrivateServiceConnectReconciler{Client: kube}).Reconcile(ctx, request(name))
	if err != nil || result.RequeueAfter != requeueFailed || taskCalls.Load() < 2 {
		t.Fatalf("async retry path did not run: task calls=%d result=%+v err=%v", taskCalls.Load(), result, err)
	}
	if strings.Contains(logs.String(), sentinel) {
		t.Fatal("SDK task retry logged sensitive response body")
	}
}

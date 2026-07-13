/*
 * SPDX-FileCopyrightText: Copyright (c) 2025-2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 * http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package namespace_scope

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/ai-dynamo/dynamo/deploy/operator/internal/features"
	coordinationv1 "k8s.io/api/coordination/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
	k8stesting "k8s.io/client-go/testing"
	"k8s.io/utils/ptr"
)

const (
	testNamespace       = "test-ns"
	testOperatorVersion = "v1.0.0"
)

func TestLeaseManager_CreateOrUpdateLease(t *testing.T) {
	existingLease := func() *coordinationv1.Lease {
		return &coordinationv1.Lease{
			ObjectMeta: metav1.ObjectMeta{
				Name:      LeaseName,
				Namespace: testNamespace,
			},
			Spec: coordinationv1.LeaseSpec{
				HolderIdentity:       ptr.To("old-holder"),
				LeaseDurationSeconds: ptr.To[int32](60),
				AcquireTime:          &metav1.MicroTime{Time: time.Now().Add(-2 * time.Minute)},
				RenewTime:            &metav1.MicroTime{Time: time.Now().Add(-1 * time.Minute)},
			},
		}
	}
	tests := []struct {
		name            string
		namespace       string
		operatorVersion string
		existingLease   *coordinationv1.Lease
	}{
		{
			name:            "creates lease with admission gates",
			namespace:       testNamespace,
			operatorVersion: testOperatorVersion,
		},
		{
			name:            "updates legacy lease with admission gates",
			namespace:       testNamespace,
			operatorVersion: testOperatorVersion,
			existingLease:   existingLease(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const admissionGatesJSON = `{"gmsSnapshot":true,"checkpoint":false}`
			const operatorPrincipal = "system:serviceaccount:test-ns:dynamo-operator"
			// Create fake client with or without existing lease
			var client *fake.Clientset
			if tt.existingLease != nil {
				client = fake.NewSimpleClientset(tt.existingLease)
			} else {
				client = fake.NewSimpleClientset()
			}

			// Create lease manager
			lm := &LeaseManager{
				client:             client,
				namespace:          tt.namespace,
				leaseDuration:      30 * time.Second,
				renewInterval:      10 * time.Second,
				holderIdentity:     "namespace-restricted-operator-" + tt.operatorVersion,
				admissionGatesJSON: admissionGatesJSON,
				operatorPrincipal:  operatorPrincipal,
			}

			// Call createOrUpdateLease
			ctx := context.Background()
			err := lm.createOrUpdateLease(ctx)
			if err != nil {
				t.Fatalf("createOrUpdateLease() error = %v", err)
			}

			// Verify lease exists
			lease, err := client.CoordinationV1().Leases(tt.namespace).Get(ctx, LeaseName, metav1.GetOptions{})
			if err != nil {
				t.Fatalf("failed to get lease: %v", err)
			}

			// Verify lease name
			if lease.Name != LeaseName {
				t.Errorf("lease name = %v, want %v", lease.Name, LeaseName)
			}

			// Verify lease namespace
			if lease.Namespace != tt.namespace {
				t.Errorf("lease namespace = %v, want %v", lease.Namespace, tt.namespace)
			}

			// Verify holder identity
			if lease.Spec.HolderIdentity == nil {
				t.Fatal("lease holder identity is nil")
			}
			wantIdentity := "namespace-restricted-operator-" + tt.operatorVersion
			if *lease.Spec.HolderIdentity != wantIdentity {
				t.Errorf("holder identity = %v, want %v", *lease.Spec.HolderIdentity, wantIdentity)
			}

			// Verify lease duration
			if lease.Spec.LeaseDurationSeconds == nil {
				t.Fatal("lease duration is nil")
			}
			if *lease.Spec.LeaseDurationSeconds != 30 {
				t.Errorf("lease duration = %v, want %v", *lease.Spec.LeaseDurationSeconds, 30)
			}
			if got := lease.Annotations[features.LeaseAnnotation]; got != admissionGatesJSON {
				t.Errorf("admission gates = %q, want %q", got, admissionGatesJSON)
			}
			if got := lease.Annotations[OperatorPrincipalAnnotation]; got != operatorPrincipal {
				t.Errorf("operator principal = %q, want %q", got, operatorPrincipal)
			}

			// Every active lease needs a RenewTime so the cluster-wide watcher
			// excludes the namespace immediately after initial creation.
			if lease.Spec.RenewTime == nil {
				t.Error("lease renew time should be set")
			} else if tt.existingLease != nil && !lease.Spec.RenewTime.After(tt.existingLease.Spec.RenewTime.Time) {
				t.Error("renew time was not updated")
			}

			// Updates preserve the original acquisition time.
			if tt.existingLease != nil && tt.existingLease.Spec.AcquireTime != nil {
				if lease.Spec.AcquireTime == nil {
					t.Error("acquire time should be preserved on update")
				} else if !lease.Spec.AcquireTime.Equal(tt.existingLease.Spec.AcquireTime) {
					t.Error("acquire time should not change on update")
				}
			}
			if lease.Spec.AcquireTime == nil {
				t.Error("lease acquire time should be set")
			}
		})
	}
}

func TestLeaseManager_StartLeavesLeaseForLeaderHandoff(t *testing.T) {
	namespace := testNamespace
	operatorVersion := testOperatorVersion

	// Create fake client
	client := fake.NewSimpleClientset()

	// Create lease manager with short intervals for testing
	lm := &LeaseManager{
		client:         client,
		namespace:      namespace,
		leaseDuration:  30 * time.Second,
		renewInterval:  50 * time.Millisecond,
		holderIdentity: "namespace-restricted-operator-" + operatorVersion,
		maxFailures:    3,
	}
	if !lm.NeedLeaderElection() {
		t.Fatal("LeaseManager must only run on the elected leader")
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- lm.Start(ctx)
	}()

	var lease *coordinationv1.Lease
	found := false
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		var err error
		lease, err = client.CoordinationV1().Leases(namespace).Get(ctx, LeaseName, metav1.GetOptions{})
		if err == nil {
			found = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !found {
		t.Fatal("lease was not created")
	}
	if lease.Name != LeaseName {
		t.Errorf("lease name = %v, want %v", lease.Name, LeaseName)
	}

	// The watcher requires RenewTime immediately so it excludes the namespace
	// before the first periodic renewal.
	if lease.Spec.RenewTime == nil {
		t.Fatal("initial lease should have renew time set on creation")
	}
	initialRenewTime := lease.Spec.RenewTime.Time

	// Poll for renewal with timeout (more robust than fixed sleep)
	renewalDetected := false
	deadline = time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		updatedLease, err := client.CoordinationV1().Leases(namespace).Get(ctx, LeaseName, metav1.GetOptions{})
		if err == nil && updatedLease.Spec.RenewTime != nil && updatedLease.Spec.RenewTime.After(initialRenewTime) {
			renewalDetected = true
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !renewalDetected {
		t.Error("lease should have renew time set after renewal")
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("LeaseManager did not stop after context cancellation")
	}

	if _, err := client.CoordinationV1().Leases(namespace).Get(context.Background(), LeaseName, metav1.GetOptions{}); err != nil {
		t.Fatalf("lease must remain for leader handoff: %v", err)
	}
}

// TestLeaseManager_FailureTracking_ReturnsErrorOnMaxFailures verifies that consecutive
// lease renewal failures stop the manager to prevent split-brain scenarios.
// Note: This test calls renewalLoop() directly (not Start()) to inject failures via reactor.
func TestLeaseManager_FailureTracking_ReturnsErrorOnMaxFailures(t *testing.T) {
	namespace := testNamespace
	operatorVersion := testOperatorVersion

	// Create existing lease
	existingLease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LeaseName,
			Namespace: namespace,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       ptr.To("namespace-restricted-operator-v1.0.0"),
			LeaseDurationSeconds: ptr.To[int32](30),
			RenewTime:            &metav1.MicroTime{Time: time.Now()},
		},
	}

	// Create fake client with the lease
	client := fake.NewSimpleClientset(existingLease)

	// Add reactor to make all update operations fail (simulates persistent API failure)
	client.PrependReactor("update", "leases", func(action k8stesting.Action) (handled bool, ret runtime.Object, err error) {
		return true, nil, fmt.Errorf("simulated persistent API failure for testing")
	})

	// Create lease manager with short intervals for faster test execution
	lm := &LeaseManager{
		client:         client,
		namespace:      namespace,
		leaseDuration:  30 * time.Second,
		renewInterval:  10 * time.Millisecond, // Fast for testing
		holderIdentity: "namespace-restricted-operator-" + operatorVersion,
		maxFailures:    3,
	}

	// Start renewal loop - all updates will fail
	ctx := context.Background()
	errCh := make(chan error, 1)
	go func() {
		errCh <- lm.renewalLoop(ctx)
	}()

	// Wait for fatal error on channel (with generous timeout)
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected error from error channel, got nil")
		}
		t.Logf("Received expected fatal error: %v", err)

		// Verify error message is meaningful
		if !strings.Contains(err.Error(), "split-brain") {
			t.Errorf("error should mention split-brain prevention, got: %v", err)
		}
		if !strings.Contains(err.Error(), "3") {
			t.Errorf("error should mention failure count, got: %v", err)
		}

	case <-time.After(1 * time.Second):
		t.Fatal("timeout waiting for fatal error from lease manager (expected within ~30ms)")
	}
}

// TestLeaseManager_FailureTracking_ResetsOnSuccess verifies that the failure counter
// is reset to 0 after a successful renewal, allowing recovery from transient failures.
// Note: This test calls renewalLoop() directly to verify internal failure counter behavior.
func TestLeaseManager_FailureTracking_ResetsOnSuccess(t *testing.T) {
	namespace := testNamespace
	operatorVersion := testOperatorVersion

	// Create fake client with existing lease
	existingLease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LeaseName,
			Namespace: namespace,
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       ptr.To("namespace-restricted-operator-v1.0.0"),
			LeaseDurationSeconds: ptr.To[int32](30),
			RenewTime:            &metav1.MicroTime{Time: time.Now()},
		},
	}
	client := fake.NewSimpleClientset(existingLease)

	// Create lease manager with pre-existing failures
	lm := &LeaseManager{
		client:         client,
		namespace:      namespace,
		leaseDuration:  30 * time.Second,
		renewInterval:  20 * time.Millisecond, // Reasonable interval for test
		holderIdentity: "namespace-restricted-operator-" + operatorVersion,
		maxFailures:    3,
		failureCount:   2, // Simulates 2 previous failures
	}

	// Start renewal loop (will succeed and reset counter)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- lm.renewalLoop(ctx)
	}()

	// Wait for at least one renewal cycle
	time.Sleep(50 * time.Millisecond)

	cancel()
	if err := <-done; err != nil {
		t.Fatalf("renewalLoop() error = %v", err)
	}

	// Verify failure count was reset to 0 after successful renewal
	if lm.failureCount != 0 {
		t.Errorf("failure count should be reset to 0 after success, got %d", lm.failureCount)
	}
}

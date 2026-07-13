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

// Package namespace_scope implements lease-based coordination for the development/test-only
// namespace-restricted operator mode.
package namespace_scope

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ai-dynamo/dynamo/deploy/operator/internal/features"
	"github.com/go-logr/logr"
	coordinationv1 "k8s.io/api/coordination/v1"
	k8sErrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// LeaseName is the well-known name for namespace reconciliation ownership leases.
	LeaseName = "dynamo-operator-namespace-scope"
	// OperatorPrincipalAnnotation stores the namespaced operator's Kubernetes username.
	OperatorPrincipalAnnotation = "nvidia.com/dynamo-operator-principal"
)

// LeaseManager maintains reconciliation ownership for namespace-restricted mode.
// Its Lease also publishes the effective admission gates and operator identity
// consumed by the cluster-wide operator. All webhooks remain global.
type LeaseManager struct {
	client             kubernetes.Interface
	namespace          string
	leaseDuration      time.Duration
	renewInterval      time.Duration
	holderIdentity     string
	admissionGatesJSON string
	operatorPrincipal  string
	failureCount       int
	maxFailures        int
	logger             logr.Logger
}

// NewLeaseManager creates a new lease manager for namespace scope marking
func NewLeaseManager(
	config *rest.Config,
	namespace string,
	operatorVersion string,
	leaseDuration time.Duration,
	renewInterval time.Duration,
	admissionGates features.Gates,
	operatorPrincipal string,
) (*LeaseManager, error) {
	// Validate inputs
	if leaseDuration <= 0 {
		return nil, fmt.Errorf("lease duration must be greater than zero, got %v", leaseDuration)
	}
	if renewInterval <= 0 {
		return nil, fmt.Errorf("renew interval must be greater than zero, got %v", renewInterval)
	}
	if renewInterval >= leaseDuration {
		return nil, fmt.Errorf("renew interval (%v) must be less than lease duration (%v)", renewInterval, leaseDuration)
	}

	client, err := kubernetes.NewForConfig(config)
	if err != nil {
		return nil, fmt.Errorf("failed to create kubernetes client: %w", err)
	}

	// Create holder identity with operator version
	// No need for pod name since there's only one operator instance in namespace-restricted mode
	holderIdentity := fmt.Sprintf("namespace-restricted-operator-%s", operatorVersion)

	// Calculate max failures with buffer to ensure operator exits BEFORE lease expires
	// This prevents split-brain: if we allow failures for the full lease duration,
	// the lease expires at exactly the same time we exit, creating a race condition.
	//
	// Strategy: Subtract 1 renewal interval as a safety buffer
	// Example: 30s lease / 10s renewal = 3 intervals
	//          maxFailures = 3 - 1 = 2 → operator exits after 20s of failures
	//          This leaves 10s buffer before lease expires at 30s
	rawMaxFailures := int(leaseDuration / renewInterval)
	maxFailures := rawMaxFailures - 1
	if maxFailures < 1 {
		maxFailures = 1 // Always allow at least 1 failure for transient issues
	}
	admissionGatesJSON, err := json.Marshal(admissionGates)
	if err != nil {
		return nil, fmt.Errorf("failed to encode admission feature gates: %w", err)
	}

	return &LeaseManager{
		client:             client,
		namespace:          namespace,
		leaseDuration:      leaseDuration,
		renewInterval:      renewInterval,
		holderIdentity:     holderIdentity,
		admissionGatesJSON: string(admissionGatesJSON),
		operatorPrincipal:  operatorPrincipal,
		maxFailures:        maxFailures,
	}, nil
}

// NeedLeaderElection makes the manager run only on the active operator leader.
func (lm *LeaseManager) NeedLeaderElection() bool {
	return true
}

// Start creates the Lease and renews it until the manager stops. The Lease is
// intentionally left to expire so a successor can renew it during leader handoff.
func (lm *LeaseManager) Start(ctx context.Context) error {
	lm.logger = log.FromContext(ctx).WithValues("component", "namespace-scope-lease", "namespace", lm.namespace)

	lm.logger.Info("Starting namespace reconciliation lease manager",
		"leaseName", LeaseName,
		"leaseDuration", lm.leaseDuration,
		"renewInterval", lm.renewInterval,
		"holderIdentity", lm.holderIdentity,
		"admissionGates", lm.admissionGatesJSON,
		"maxFailures", lm.maxFailures)

	// Create or update the lease initially
	if err := lm.createOrUpdateLease(ctx); err != nil {
		return fmt.Errorf("failed to create initial lease: %w", err)
	}

	lm.logger.Info("Namespace reconciliation lease created successfully")
	return lm.renewalLoop(ctx)
}

// createOrUpdateLease creates or updates the namespace reconciliation lease.
func (lm *LeaseManager) createOrUpdateLease(ctx context.Context) error {
	now := metav1.NewMicroTime(time.Now())
	leaseDurationSeconds := int32(lm.leaseDuration.Seconds())

	lease := &coordinationv1.Lease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      LeaseName,
			Namespace: lm.namespace,
			Annotations: map[string]string{
				features.LeaseAnnotation:    lm.admissionGatesJSON,
				OperatorPrincipalAnnotation: lm.operatorPrincipal,
			},
		},
		Spec: coordinationv1.LeaseSpec{
			HolderIdentity:       &lm.holderIdentity,
			LeaseDurationSeconds: &leaseDurationSeconds,
			AcquireTime:          &now,
			RenewTime:            &now,
		},
	}

	// Try to get existing lease
	existingLease, err := lm.client.CoordinationV1().Leases(lm.namespace).Get(ctx, LeaseName, metav1.GetOptions{})
	if err != nil {
		if !k8sErrors.IsNotFound(err) {
			return fmt.Errorf("failed to get lease: %w", err)
		}
		// Lease doesn't exist, create it
		_, err = lm.client.CoordinationV1().Leases(lm.namespace).Create(ctx, lease, metav1.CreateOptions{})
		if err != nil {
			return fmt.Errorf("failed to create lease: %w", err)
		}
		lm.logger.Info("Created namespace reconciliation lease")
		return nil
	}

	// Lease exists, update it
	existingLease.Spec.HolderIdentity = &lm.holderIdentity
	existingLease.Spec.LeaseDurationSeconds = &leaseDurationSeconds
	existingLease.Spec.RenewTime = &now
	if existingLease.Annotations == nil {
		existingLease.Annotations = make(map[string]string)
	}
	existingLease.Annotations[features.LeaseAnnotation] = lm.admissionGatesJSON
	existingLease.Annotations[OperatorPrincipalAnnotation] = lm.operatorPrincipal

	_, err = lm.client.CoordinationV1().Leases(lm.namespace).Update(ctx, existingLease, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update lease: %w", err)
	}

	lm.logger.V(1).Info("Refreshed namespace reconciliation lease")
	return nil
}

// renewalLoop continuously renews the lease until stopped.
func (lm *LeaseManager) renewalLoop(ctx context.Context) error {
	ticker := time.NewTicker(lm.renewInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			lm.logger.Info("Context cancelled, stopping lease renewal loop")
			return nil
		case <-ticker.C:
			// Use createOrUpdateLease instead of renewLease for self-healing
			// If the lease is manually deleted, it will be automatically recreated
			if err := lm.createOrUpdateLease(ctx); err != nil {
				lm.failureCount++
				lm.logger.Error(err, "Failed to create/update lease, will retry",
					"failureCount", lm.failureCount,
					"maxFailures", lm.maxFailures,
					"nextRetry", lm.renewInterval)

				// Warn when approaching max failures
				if lm.failureCount == lm.maxFailures-1 {
					lm.logger.Error(nil, "WARNING: One more lease renewal failure will cause operator shutdown to prevent split-brain",
						"failureCount", lm.failureCount,
						"maxFailures", lm.maxFailures)
				}

				// After max consecutive failures, stop the manager to prevent split-brain.
				if lm.failureCount >= lm.maxFailures {
					fatalErr := fmt.Errorf("lease renewal failed %d consecutive times (max: %d), operator must exit to prevent split-brain with cluster-wide operator", lm.failureCount, lm.maxFailures)
					lm.logger.Error(fatalErr, "FATAL: Max lease renewal failures exceeded")
					return fatalErr
				}
			} else {
				// Success: reset failure counter
				if lm.failureCount > 0 {
					lm.logger.Info("Lease renewal recovered after failures",
						"previousFailures", lm.failureCount)
					lm.failureCount = 0
				}
			}
		}
	}
}

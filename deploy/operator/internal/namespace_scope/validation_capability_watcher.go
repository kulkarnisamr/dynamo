/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package namespace_scope

import (
	"context"
	"errors"
	"fmt"

	"github.com/ai-dynamo/dynamo/deploy/operator/internal/features"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

const (
	partOfLabel = "app.kubernetes.io/part-of"
	partOfValue = "dynamo-operator"
)

// RequireClusterWideAdmissionCapability verifies that a namespaced operator can
// publish gate overrides without creating an admission gap.
func RequireClusterWideAdmissionCapability(ctx context.Context, client kubernetes.Interface) error {
	selector := labels.Set{
		partOfLabel:                       partOfValue,
		features.AdmissionCapabilityLabel: features.AdmissionCapabilityV1,
	}.String()
	configurations, err := client.AdmissionregistrationV1().ValidatingWebhookConfigurations().List(
		ctx,
		metav1.ListOptions{LabelSelector: selector},
	)
	if err != nil {
		return fmt.Errorf("listing cluster-wide validating webhook configurations: %w", err)
	}
	for idx := range configurations.Items {
		if isClusterWideAdmissionCapability(&configurations.Items[idx]) {
			return nil
		}
	}
	return errors.New("no cluster-wide Dynamo validating webhook supports namespaced admission feature gates")
}

func isClusterWideAdmissionCapability(configuration *admissionregistrationv1.ValidatingWebhookConfiguration) bool {
	if configuration.Labels[partOfLabel] != partOfValue ||
		configuration.Labels[features.AdmissionCapabilityLabel] != features.AdmissionCapabilityV1 ||
		len(configuration.Webhooks) == 0 {
		return false
	}
	for idx := range configuration.Webhooks {
		selector := configuration.Webhooks[idx].NamespaceSelector
		if selector != nil && (len(selector.MatchLabels) > 0 || len(selector.MatchExpressions) > 0) {
			return false
		}
	}
	return true
}

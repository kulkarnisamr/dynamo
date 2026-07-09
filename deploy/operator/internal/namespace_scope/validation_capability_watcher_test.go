/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package namespace_scope

import (
	"context"
	"testing"

	"github.com/ai-dynamo/dynamo/deploy/operator/internal/features"
	admissionregistrationv1 "k8s.io/api/admissionregistration/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
)

func TestRequireClusterWideAdmissionCapability(t *testing.T) {
	configuration := &admissionregistrationv1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cluster-wide",
			Labels: map[string]string{
				partOfLabel:                       partOfValue,
				features.AdmissionCapabilityLabel: features.AdmissionCapabilityV1,
			},
		},
		Webhooks: []admissionregistrationv1.ValidatingWebhook{{Name: "validation.dynamo.nvidia.com"}},
	}
	if err := RequireClusterWideAdmissionCapability(context.Background(), fake.NewSimpleClientset(configuration)); err != nil {
		t.Fatalf("capable global configuration was rejected: %v", err)
	}

	namespaced := configuration.DeepCopy()
	namespaced.Webhooks[0].NamespaceSelector = &metav1.LabelSelector{
		MatchLabels: map[string]string{"kubernetes.io/metadata.name": "tenant-a"},
	}
	if err := RequireClusterWideAdmissionCapability(context.Background(), fake.NewSimpleClientset(namespaced)); err == nil {
		t.Fatal("namespace-scoped webhook configuration must not satisfy the capability")
	}
	if err := RequireClusterWideAdmissionCapability(context.Background(), fake.NewSimpleClientset()); err == nil {
		t.Fatal("missing global admission capability must be rejected")
	}
}

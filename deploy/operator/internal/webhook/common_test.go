/*
 * SPDX-FileCopyrightText: Copyright (c) 2025-2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package webhook

import (
	"context"
	"slices"
	"testing"

	"github.com/ai-dynamo/dynamo/deploy/operator/internal/features"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

type staticGateResolver map[string]features.Gates

func (s staticGateResolver) ForNamespace(namespace string) (features.Gates, []string) {
	gates, found := s[namespace]
	if !found {
		return gates, nil
	}
	return gates, []string{"resolver warning"}
}

type recordingValidator struct {
	gates []features.Gates
}

func (v *recordingValidator) validate(ctx context.Context) (admission.Warnings, error) {
	v.gates = append(v.gates, features.MustFromContext(ctx))
	return admission.Warnings{"validator warning"}, nil
}

func (v *recordingValidator) ValidateCreate(ctx context.Context, _ runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx)
}

func (v *recordingValidator) ValidateUpdate(ctx context.Context, _, _ runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx)
}

func (v *recordingValidator) ValidateDelete(ctx context.Context, _ runtime.Object) (admission.Warnings, error) {
	return v.validate(ctx)
}

func TestFeatureAwareValidator(t *testing.T) {
	recording := &recordingValidator{}
	validator := NewFeatureAwareValidator(recording, staticGateResolver{
		"claimed": {Grove: true},
	})
	claimed := &corev1.ConfigMap{}
	claimed.Namespace = "claimed"
	calls := []func() (admission.Warnings, error){
		func() (admission.Warnings, error) { return validator.ValidateCreate(context.Background(), claimed) },
		func() (admission.Warnings, error) {
			return validator.ValidateUpdate(context.Background(), &corev1.ConfigMap{}, claimed)
		},
		func() (admission.Warnings, error) { return validator.ValidateDelete(context.Background(), claimed) },
	}

	for _, call := range calls {
		warnings, err := call()
		if err != nil || !slices.Equal(warnings, admission.Warnings{"resolver warning", "validator warning"}) {
			t.Fatalf("validation = %v, %v", warnings, err)
		}
	}
	for _, gates := range recording.gates {
		if !gates.Grove {
			t.Fatalf("validator received gates %#v, want Grove enabled", gates)
		}
	}
}

func TestCanModifyDGDReplicas(t *testing.T) {
	tests := []struct {
		name               string
		principal          string
		namespacePrincipal string
		username           string
		expectAllowed      bool
	}{
		{
			name:          "operator SA with standard Helm release (dynamo-platform)",
			principal:     "system:serviceaccount:dynamo-system:dynamo-platform-dynamo-operator-controller-manager",
			username:      "system:serviceaccount:dynamo-system:dynamo-platform-dynamo-operator-controller-manager",
			expectAllowed: true,
		},
		{
			name:          "operator SA with collapsed Helm release (dynamo-operator) — the bug scenario",
			principal:     "system:serviceaccount:dynamo-system:dynamo-operator-controller-manager",
			username:      "system:serviceaccount:dynamo-system:dynamo-operator-controller-manager",
			expectAllowed: true,
		},
		{
			name:          "operator SA auto-detected from downward API",
			principal:     "system:serviceaccount:custom-ns:my-release-controller-manager",
			username:      "system:serviceaccount:custom-ns:my-release-controller-manager",
			expectAllowed: true,
		},
		{
			name:               "active namespaced operator SA",
			principal:          "system:serviceaccount:dynamo-system:dynamo-operator-controller-manager",
			namespacePrincipal: "system:serviceaccount:tenant-a:dev-operator-controller-manager",
			username:           "system:serviceaccount:tenant-a:dev-operator-controller-manager",
			expectAllowed:      true,
		},
		{
			name:          "operator SA wrong namespace is rejected",
			principal:     "system:serviceaccount:dynamo-system:dynamo-operator-controller-manager",
			username:      "system:serviceaccount:other-ns:dynamo-operator-controller-manager",
			expectAllowed: false,
		},
		{
			name:          "planner SA allowed in any namespace (well-known name)",
			principal:     "system:serviceaccount:dynamo-system:dynamo-operator-controller-manager",
			username:      "system:serviceaccount:user-ns:planner-serviceaccount",
			expectAllowed: true,
		},
		{
			name:          "planner SA allowed with no operator principal set",
			principal:     "",
			username:      "system:serviceaccount:other-ns:planner-serviceaccount",
			expectAllowed: true,
		},
		{
			name:          "unauthorized SA rejected",
			principal:     "system:serviceaccount:dynamo-system:dynamo-operator-controller-manager",
			username:      "system:serviceaccount:user-ns:some-random-sa",
			expectAllowed: false,
		},
		{
			name:          "non-SA user rejected",
			principal:     "system:serviceaccount:dynamo-system:dynamo-operator-controller-manager",
			username:      "admin@example.com",
			expectAllowed: false,
		},
		{
			name:          "malformed SA username rejected",
			principal:     "system:serviceaccount:dynamo-system:dynamo-operator-controller-manager",
			username:      "system:serviceaccount:only-three-parts",
			expectAllowed: false,
		},
		{
			name:          "empty operator principal still permits planner",
			principal:     "",
			username:      "system:serviceaccount:ns:planner-serviceaccount",
			expectAllowed: true,
		},
		{
			name:          "empty operator principal rejects other SA",
			principal:     "",
			username:      "system:serviceaccount:ns:dynamo-operator-controller-manager",
			expectAllowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userInfo := authenticationv1.UserInfo{Username: tt.username}
			got := CanModifyDGDReplicas([]string{tt.principal, tt.namespacePrincipal}, userInfo)
			if got != tt.expectAllowed {
				t.Errorf("CanModifyDGDReplicas() = %v, want %v", got, tt.expectAllowed)
			}
		})
	}
}

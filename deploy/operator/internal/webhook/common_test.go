/*
 * SPDX-FileCopyrightText: Copyright (c) 2025-2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package webhook

import (
	"context"
	"errors"
	"testing"

	"github.com/ai-dynamo/dynamo/deploy/operator/internal/features"
	authenticationv1 "k8s.io/api/authentication/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var errValidationCalled = errors.New("validation called")

type staticGateResolver map[string]features.Gates

func (s staticGateResolver) ForNamespace(namespace string) (features.Gates, []string) {
	warnings := []string(nil)
	if namespace == "claimed" {
		warnings = []string{"unknown feature gate"}
	}
	return s[namespace], warnings
}

type rejectingValidator struct{}

func (rejectingValidator) ValidateCreate(ctx context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, validationCalled(ctx)
}

func (rejectingValidator) ValidateUpdate(ctx context.Context, _, _ runtime.Object) (admission.Warnings, error) {
	return nil, validationCalled(ctx)
}

func (rejectingValidator) ValidateDelete(ctx context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, validationCalled(ctx)
}

func validationCalled(ctx context.Context) error {
	features.MustFromContext(ctx)
	return errValidationCalled
}

func TestFeatureAwareValidator(t *testing.T) {
	validator := NewFeatureAwareValidator(rejectingValidator{}, staticGateResolver{
		"claimed": {Grove: true},
	})
	claimed := &corev1.ConfigMap{}
	claimed.Namespace = "claimed"
	unclaimed := &corev1.ConfigMap{}
	unclaimed.Namespace = "unclaimed"

	tests := []struct {
		name         string
		call         func() (admission.Warnings, error)
		wantWarnings int
	}{
		{
			name: "validates create with namespaced gates",
			call: func() (admission.Warnings, error) {
				return validator.ValidateCreate(context.Background(), claimed)
			},
			wantWarnings: 1,
		},
		{
			name: "validates update with namespaced gates",
			call: func() (admission.Warnings, error) {
				return validator.ValidateUpdate(context.Background(), unclaimed, claimed)
			},
			wantWarnings: 1,
		},
		{
			name: "validates delete with namespaced gates",
			call: func() (admission.Warnings, error) {
				return validator.ValidateDelete(context.Background(), claimed)
			},
			wantWarnings: 1,
		},
		{
			name: "validates namespace with global gates",
			call: func() (admission.Warnings, error) {
				return validator.ValidateCreate(context.Background(), unclaimed)
			},
		},
		{
			name: "validates object without metadata",
			call: func() (admission.Warnings, error) {
				return validator.ValidateCreate(context.Background(), &runtime.Unknown{})
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			warnings, err := test.call()
			if !errors.Is(err, errValidationCalled) {
				t.Errorf("validation error = %v, want %v", err, errValidationCalled)
			}
			if len(warnings) != test.wantWarnings {
				t.Errorf("warnings = %v, want %d", warnings, test.wantWarnings)
			}
		})
	}
}

func TestCanModifyDGDReplicas(t *testing.T) {
	tests := []struct {
		name          string
		principal     string
		username      string
		expectAllowed bool
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
			got := CanModifyDGDReplicas(tt.principal, userInfo)
			if got != tt.expectAllowed {
				t.Errorf("CanModifyDGDReplicas() = %v, want %v", got, tt.expectAllowed)
			}
		})
	}
}

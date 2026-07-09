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

package webhook

import (
	"context"
	"strings"

	"github.com/ai-dynamo/dynamo/deploy/operator/internal/consts"
	"github.com/ai-dynamo/dynamo/deploy/operator/internal/features"
	authenticationv1 "k8s.io/api/authentication/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var webhookCommonLog = logf.Log.WithName("webhook-common")

var validationResolver features.Resolver

// SetValidationResolver sets the feature resolver consulted by validating webhooks.
// It must be called before webhook handlers are registered.
func SetValidationResolver(resolver features.Resolver) {
	validationResolver = resolver
}

// ValidationResolver returns the feature resolver used by validating webhooks.
func ValidationResolver() features.Resolver {
	return validationResolver
}

// FeatureAwareValidator supplies the effective namespace gates to a validator.
type FeatureAwareValidator struct {
	validator admission.CustomValidator
	resolver  features.Resolver
}

// NewFeatureAwareValidator applies namespace gate overrides while always running
// the cluster-wide validator.
func NewFeatureAwareValidator(
	validator admission.CustomValidator,
	resolver features.Resolver,
) admission.CustomValidator {
	return &FeatureAwareValidator{
		validator: validator,
		resolver:  resolver,
	}
}

// ValidateCreate implements admission.CustomValidator.
func (v *FeatureAwareValidator) ValidateCreate(
	ctx context.Context,
	obj runtime.Object,
) (admission.Warnings, error) {
	ctx, gateWarnings := v.contextFor(ctx, obj)
	warnings, err := v.validator.ValidateCreate(ctx, obj)
	return append(gateWarnings, warnings...), err
}

// ValidateUpdate implements admission.CustomValidator.
func (v *FeatureAwareValidator) ValidateUpdate(
	ctx context.Context,
	oldObj runtime.Object,
	newObj runtime.Object,
) (admission.Warnings, error) {
	ctx, gateWarnings := v.contextFor(ctx, newObj)
	warnings, err := v.validator.ValidateUpdate(ctx, oldObj, newObj)
	return append(gateWarnings, warnings...), err
}

// ValidateDelete implements admission.CustomValidator.
func (v *FeatureAwareValidator) ValidateDelete(
	ctx context.Context,
	obj runtime.Object,
) (admission.Warnings, error) {
	ctx, gateWarnings := v.contextFor(ctx, obj)
	warnings, err := v.validator.ValidateDelete(ctx, obj)
	return append(gateWarnings, warnings...), err
}

func (v *FeatureAwareValidator) contextFor(
	ctx context.Context,
	obj runtime.Object,
) (context.Context, admission.Warnings) {
	if v.resolver == nil {
		return ctx, nil
	}
	namespace := ""
	clientObj, ok := obj.(client.Object)
	if ok {
		namespace = clientObj.GetNamespace()
	}
	gates, warnings := v.resolver.ForNamespace(namespace)
	return features.WithGates(ctx, gates), warnings
}

// CanModifyDGDReplicas checks if the request comes from a service account authorized
// to modify DGD replicas when scaling adapter is enabled.
//
// operatorPrincipal is the full Kubernetes username
// (system:serviceaccount:<namespace>:<name>) of the operator's own service account,
// auto-detected at startup via the Kubernetes Downward API. It may be empty if
// the Downward API env vars were not set.
//
// Authorization is checked in two ways:
//  1. Exact match against operatorPrincipal.
//  2. Name-only match for the planner SA, which the operator creates in every DGD
//     namespace with a well-known constant name. Because the namespace is only known
//     at runtime, it cannot be enumerated statically.
func CanModifyDGDReplicas(operatorPrincipal string, userInfo authenticationv1.UserInfo) bool {
	username := userInfo.Username

	if !strings.HasPrefix(username, "system:serviceaccount:") {
		return false
	}

	if operatorPrincipal != "" && username == operatorPrincipal {
		webhookCommonLog.V(1).Info("allowing DGD replicas modification",
			"username", username,
			"matchType", "operatorPrincipal")
		return true
	}

	parts := strings.Split(username, ":")
	if len(parts) == 4 && parts[3] == consts.PlannerServiceAccountName {
		webhookCommonLog.V(1).Info("allowing DGD replicas modification",
			"username", username,
			"matchType", "plannerSA")
		return true
	}

	return false
}

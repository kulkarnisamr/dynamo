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

package validation

import (
	"context"
	"fmt"

	nvidiacomv1beta1 "github.com/ai-dynamo/dynamo/deploy/operator/api/v1beta1"
	"github.com/ai-dynamo/dynamo/deploy/operator/internal/consts"
	"github.com/ai-dynamo/dynamo/deploy/operator/internal/features"
	"github.com/ai-dynamo/dynamo/deploy/operator/internal/observability"
	internalwebhook "github.com/ai-dynamo/dynamo/deploy/operator/internal/webhook"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const (
	// DynamoGraphDeploymentRequestWebhookName is the name of the validating webhook handler for DynamoGraphDeploymentRequest.
	DynamoGraphDeploymentRequestWebhookName = "dynamographdeploymentrequest-validating-webhook"
	dynamoGraphDeploymentRequestWebhookPath = "/validate-nvidia-com-v1beta1-dynamographdeploymentrequest"
)

// DynamoGraphDeploymentRequestHandler is a handler for validating DynamoGraphDeploymentRequest resources.
// It is a thin wrapper around DynamoGraphDeploymentRequestValidator.
type DynamoGraphDeploymentRequestHandler struct {
	isClusterWideOperator bool
	gpuDiscoveryEnabled   bool
}

// NewDynamoGraphDeploymentRequestHandler creates a new handler for DynamoGraphDeploymentRequest Webhook.
// isClusterWide indicates whether the operator has cluster-wide permissions.
// gpuDiscoveryEnabled indicates whether a ClusterRole for node read access was provisioned by Helm.
func NewDynamoGraphDeploymentRequestHandler(isClusterWide bool, gpuDiscoveryEnabled bool) *DynamoGraphDeploymentRequestHandler {
	return &DynamoGraphDeploymentRequestHandler{
		isClusterWideOperator: isClusterWide,
		gpuDiscoveryEnabled:   gpuDiscoveryEnabled,
	}
}

// ValidateCreate validates a DynamoGraphDeploymentRequest create request.
func (h *DynamoGraphDeploymentRequestHandler) ValidateCreate(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	logger := log.FromContext(ctx).WithName(DynamoGraphDeploymentRequestWebhookName)

	if err := internalwebhook.ValidateAdmissionGVK(ctx, nvidiacomv1beta1.DynamoGraphDeploymentRequestGVK); err != nil {
		return nil, err
	}

	request, err := castToDynamoGraphDeploymentRequest(obj)
	if err != nil {
		return nil, err
	}

	logger.Info("validate create", "name", request.Name, "namespace", request.Namespace)

	// Create validator and perform validation
	isClusterWide, gpuDiscoveryEnabled := h.effectiveGPUDiscovery(ctx)
	validator := NewDynamoGraphDeploymentRequestValidator(request, isClusterWide, gpuDiscoveryEnabled)
	return validator.Validate()
}

// ValidateUpdate validates a DynamoGraphDeploymentRequest update request.
func (h *DynamoGraphDeploymentRequestHandler) ValidateUpdate(ctx context.Context, oldObj, newObj runtime.Object) (admission.Warnings, error) {
	logger := log.FromContext(ctx).WithName(DynamoGraphDeploymentRequestWebhookName)

	if err := internalwebhook.ValidateAdmissionGVK(ctx, nvidiacomv1beta1.DynamoGraphDeploymentRequestGVK); err != nil {
		return nil, err
	}

	newRequest, err := castToDynamoGraphDeploymentRequest(newObj)
	if err != nil {
		return nil, err
	}

	logger.Info("validate update", "name", newRequest.Name, "namespace", newRequest.Namespace)

	// Skip validation if the resource is being deleted (to allow finalizer removal)
	if !newRequest.DeletionTimestamp.IsZero() {
		logger.Info("skipping validation for resource being deleted", "name", newRequest.Name)
		return nil, nil
	}

	oldRequest, err := castToDynamoGraphDeploymentRequest(oldObj)
	if err != nil {
		return nil, err
	}

	// Create validator and perform validation
	isClusterWide, gpuDiscoveryEnabled := h.effectiveGPUDiscovery(ctx)
	validator := NewDynamoGraphDeploymentRequestValidator(newRequest, isClusterWide, gpuDiscoveryEnabled)
	return validator.ValidateUpdate(oldRequest)
}

// ValidateDelete validates a DynamoGraphDeploymentRequest delete request.
func (h *DynamoGraphDeploymentRequestHandler) ValidateDelete(ctx context.Context, obj runtime.Object) (admission.Warnings, error) {
	logger := log.FromContext(ctx).WithName(DynamoGraphDeploymentRequestWebhookName)

	if err := internalwebhook.ValidateAdmissionGVK(ctx, nvidiacomv1beta1.DynamoGraphDeploymentRequestGVK); err != nil {
		return nil, err
	}

	request, err := castToDynamoGraphDeploymentRequest(obj)
	if err != nil {
		return nil, err
	}

	logger.Info("validate delete", "name", request.Name, "namespace", request.Namespace)

	// No special validation needed for deletion
	return nil, nil
}

func (h *DynamoGraphDeploymentRequestHandler) effectiveGPUDiscovery(ctx context.Context) (bool, bool) {
	if gates, ok := features.FromContext(ctx); ok {
		return false, gates.GPUDiscovery
	}
	return h.isClusterWideOperator, h.gpuDiscoveryEnabled
}

// RegisterWithManager registers the webhook with the manager.
func (h *DynamoGraphDeploymentRequestHandler) RegisterWithManager(mgr manager.Manager) error {
	featureAwareValidator := internalwebhook.NewFeatureAwareValidator(h, internalwebhook.ValidationResolver())

	// Wrap with metrics collection
	observedValidator := observability.NewObservedValidator(featureAwareValidator, consts.ResourceTypeDynamoGraphDeploymentRequest)

	webhook := admission.
		WithCustomValidator(mgr.GetScheme(), &nvidiacomv1beta1.DynamoGraphDeploymentRequest{}, observedValidator).
		WithRecoverPanic(true)
	mgr.GetWebhookServer().Register(dynamoGraphDeploymentRequestWebhookPath, webhook)
	return nil
}

// castToDynamoGraphDeploymentRequest attempts to cast a runtime.Object to a DynamoGraphDeploymentRequest.
func castToDynamoGraphDeploymentRequest(obj runtime.Object) (*nvidiacomv1beta1.DynamoGraphDeploymentRequest, error) {
	request, ok := obj.(*nvidiacomv1beta1.DynamoGraphDeploymentRequest)
	if !ok {
		return nil, fmt.Errorf("expected DynamoGraphDeploymentRequest but got %T", obj)
	}
	return request, nil
}

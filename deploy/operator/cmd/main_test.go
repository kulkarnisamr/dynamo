/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package main

import (
	"strings"
	"testing"

	configv1alpha1 "github.com/ai-dynamo/dynamo/deploy/operator/api/config/v1alpha1"
)

func TestRegisterWebhookHandlersRejectsNamespacedMode(t *testing.T) {
	operatorConfig := &configv1alpha1.OperatorConfiguration{
		Namespace: configv1alpha1.NamespaceConfiguration{Restricted: "tenant-a"},
	}

	err := registerWebhookHandlers(nil, operatorConfig, "test", "", nil)
	if err == nil || !strings.Contains(err.Error(), "cluster-wide operator") {
		t.Fatalf("expected cluster-wide ownership error, got %v", err)
	}
}

func TestOperatorServiceAccountPrincipal(t *testing.T) {
	t.Setenv("POD_SERVICE_ACCOUNT", "dev-operator")
	t.Setenv("POD_NAMESPACE", "tenant-a")
	if got := operatorServiceAccountPrincipal(); got != "system:serviceaccount:tenant-a:dev-operator" {
		t.Fatalf("operatorServiceAccountPrincipal() = %q", got)
	}

	t.Setenv("POD_NAMESPACE", "")
	if got := operatorServiceAccountPrincipal(); got != "" {
		t.Fatalf("operatorServiceAccountPrincipal() without namespace = %q", got)
	}
}

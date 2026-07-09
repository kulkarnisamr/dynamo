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

	err := registerWebhookHandlers(nil, operatorConfig, nil, "test")
	if err == nil || !strings.Contains(err.Error(), "cluster-wide operator") {
		t.Fatalf("expected cluster-wide ownership error, got %v", err)
	}
}

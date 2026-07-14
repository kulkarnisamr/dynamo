/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package controller_common

import (
	"context"
	"testing"
	"time"

	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type excludedNamespaces map[string]bool

func (e excludedNamespaces) Contains(namespace string) bool {
	return e[namespace]
}

func TestWithNamespaceExclusion(t *testing.T) {
	calls := 0
	delegate := reconcile.Func(func(context.Context, ctrl.Request) (ctrl.Result, error) {
		calls++
		return ctrl.Result{RequeueAfter: time.Second}, nil
	})
	runtimeConfig := &RuntimeConfig{ExcludedNamespaces: excludedNamespaces{"tenant-a": true}}
	reconciler := WithNamespaceExclusion(delegate, runtimeConfig)

	result, err := reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "tenant-a", Name: "object"},
	})
	if err != nil || result.RequeueAfter != namespaceExclusionRequeueAfter || calls != 0 {
		t.Fatalf("excluded reconcile = (%v, %v, %d calls), want bounded requeue, nil error, no calls", result, err, calls)
	}

	result, err = reconciler.Reconcile(context.Background(), ctrl.Request{
		NamespacedName: types.NamespacedName{Namespace: "tenant-b", Name: "object"},
	})
	if err != nil || result.RequeueAfter != time.Second || calls != 1 {
		t.Fatalf("included reconcile = (%v, %v, %d calls), want requeue result, nil error, one call", result, err, calls)
	}
}

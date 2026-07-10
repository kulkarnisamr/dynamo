/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package controller_common

import (
	"context"
	"time"

	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

const namespaceExclusionRequeueAfter = 10 * time.Second

type namespaceFilteringReconciler struct {
	delegate      reconcile.Reconciler
	runtimeConfig *RuntimeConfig
}

// WithNamespaceExclusion prevents queued requests for namespaces managed by a
// namespaced operator from reaching the wrapped reconciler.
func WithNamespaceExclusion(delegate reconcile.Reconciler, runtimeConfig *RuntimeConfig) reconcile.Reconciler {
	return &namespaceFilteringReconciler{delegate: delegate, runtimeConfig: runtimeConfig}
}

func (r *namespaceFilteringReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	if req.Namespace != "" && r.runtimeConfig != nil && r.runtimeConfig.ExcludedNamespaces != nil &&
		r.runtimeConfig.ExcludedNamespaces.Contains(req.Namespace) {
		log.FromContext(ctx).V(1).Info(
			"Skipping reconciliation because namespace is managed by a namespaced operator",
			"namespace", req.Namespace,
		)

		// Keep the request alive so reconciliation resumes after the namespaced
		// operator's lease expires or is deleted.
		return ctrl.Result{RequeueAfter: namespaceExclusionRequeueAfter}, nil
	}
	return r.delegate.Reconcile(ctx, req)
}

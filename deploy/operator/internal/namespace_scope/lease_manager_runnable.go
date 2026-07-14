/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package namespace_scope

import (
	"context"

	"sigs.k8s.io/controller-runtime/pkg/manager"
)

type leaderElectedLeaseManager struct {
	leaseManager *LeaseManager
}

// WithLeaderElection runs the namespace Lease only on the controller-runtime leader.
func WithLeaderElection(leaseManager *LeaseManager) manager.Runnable {
	return &leaderElectedLeaseManager{leaseManager: leaseManager}
}

func (r *leaderElectedLeaseManager) NeedLeaderElection() bool {
	return true
}

func (r *leaderElectedLeaseManager) Start(ctx context.Context) error {
	if err := r.leaseManager.Start(ctx); err != nil {
		return err
	}

	select {
	case err := <-r.leaseManager.Errors():
		return err
	case <-ctx.Done():
		r.leaseManager.wg.Wait()
		return nil
	}
}

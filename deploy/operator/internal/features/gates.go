/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

// Package features defines operator feature gates.
package features

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// GMSSnapshotEnvVar enables the temporary internal GMS + Snapshot gate when set to "1".
const GMSSnapshotEnvVar = "DYN_OPERATOR_ALLOW_GMS_SNAPSHOT"

// FromEnvironment returns feature gates controlled directly by process environment.
func FromEnvironment() Gates {
	return Gates{
		GMSSnapshot: os.Getenv(GMSSnapshotEnvVar) == "1",
	}
}

const (
	// LeaseAnnotation stores the namespaced operator's effective feature gates.
	LeaseAnnotation = "nvidia.com/dynamo-operator-admission-feature-gates"
	// AdmissionCapabilityLabel marks global admission that understands LeaseAnnotation.
	AdmissionCapabilityLabel = "nvidia.com/dynamo-operator-lease-admission-feature-gates"
	// AdmissionCapabilityV1 is the first supported Lease gate protocol.
	AdmissionCapabilityV1 = "v1"
)

// Gates is the complete effective set of operator feature gates.
// Fields intentionally do not use omitempty: false is an explicit namespaced override.
type Gates struct {
	GMSSnapshot      bool `json:"gmsSnapshot"`
	Checkpoint       bool `json:"checkpoint"`
	Grove            bool `json:"grove"`
	LWS              bool `json:"lws"`
	KaiScheduler     bool `json:"kaiScheduler"`
	VolcanoScheduler bool `json:"volcanoScheduler"`
	DRA              bool `json:"dra"`
	Istio            bool `json:"istio"`
	GPUDiscovery     bool `json:"gpuDiscovery"`
}

// Resolver returns the effective gates and compatibility warnings for a namespace.
type Resolver interface {
	ForNamespace(namespace string) (Gates, []string)
}

type resolver struct {
	base   Gates
	source func(namespace string) (string, bool)
}

// NewResolver creates a namespace-aware gate resolver.
func NewResolver(base Gates, source func(namespace string) (string, bool)) Resolver {
	return &resolver{base: base, source: source}
}

func (r *resolver) ForNamespace(namespace string) (Gates, []string) {
	if r.source == nil {
		return r.base, nil
	}
	snapshot, found := r.source(namespace)
	if !found {
		return r.base, nil
	}
	resolved, unknown, err := resolve(r.base, snapshot)
	if err != nil {
		return r.base, []string{fmt.Sprintf(
			"ignoring invalid admission feature gates published for namespace %q: %v",
			namespace,
			err,
		)}
	}
	if len(unknown) == 0 {
		return resolved, nil
	}
	return resolved, []string{fmt.Sprintf(
		"namespace operator requested feature gates unknown to the cluster-wide operator: %s",
		strings.Join(unknown, ", "),
	)}
}

// resolve overlays a JSON gate snapshot onto base. Missing fields inherit base,
// allowing an older namespaced operator to coexist with a newer global operator.
func resolve(base Gates, snapshot string) (Gates, []string, error) {
	if snapshot == "" {
		return base, nil, nil
	}

	resolved := base
	if err := json.Unmarshal([]byte(snapshot), &resolved); err != nil {
		return base, nil, fmt.Errorf("decoding admission feature gates: %w", err)
	}

	decoder := json.NewDecoder(strings.NewReader(snapshot))
	decoder.DisallowUnknownFields()
	strict := base
	if err := decoder.Decode(&strict); err != nil {
		const prefix = "json: unknown field \""
		if message := err.Error(); strings.HasPrefix(message, prefix) && strings.HasSuffix(message, "\"") {
			return resolved, []string{strings.TrimSuffix(strings.TrimPrefix(message, prefix), "\"")}, nil
		}
		return base, nil, fmt.Errorf("decoding admission feature gates: %w", err)
	}

	return resolved, nil, nil
}

type gatesContextKey struct{}

// WithGates attaches the effective admission gates to a request context.
func WithGates(ctx context.Context, gates Gates) context.Context {
	return context.WithValue(ctx, gatesContextKey{}, gates)
}

// MustFromContext returns the request's effective admission gates.
// Admission handlers must be wrapped by FeatureAwareValidator before use.
func MustFromContext(ctx context.Context) Gates {
	gates, ok := ctx.Value(gatesContextKey{}).(Gates)
	if !ok {
		panic("feature gates missing from admission context")
	}
	return gates
}

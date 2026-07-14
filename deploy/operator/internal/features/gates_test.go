/*
 * SPDX-FileCopyrightText: Copyright (c) 2026 NVIDIA CORPORATION & AFFILIATES. All rights reserved.
 * SPDX-License-Identifier: Apache-2.0
 */

package features

import (
	"context"
	"encoding/json"
	"testing"
)

func TestFromEnvironment(t *testing.T) {
	t.Setenv(GMSSnapshotEnvVar, "1")
	if !FromEnvironment().GMSSnapshot {
		t.Fatal("GMS Snapshot environment gate was not enabled")
	}
}

func TestGatesJSONIncludesDisabledValues(t *testing.T) {
	data, err := json.Marshal(Gates{})
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(data), `{"gmsSnapshot":false,"checkpoint":false,"grove":false,"lws":false,"kaiScheduler":false,"volcanoScheduler":false,"dra":false,"istio":false,"gpuDiscovery":false}`; got != want {
		t.Fatalf("Gates JSON = %s, want %s", got, want)
	}
}

type staticSnapshotSource map[string]string

func (s staticSnapshotSource) AdmissionGateSnapshot(namespace string) (string, bool) {
	snapshot, found := s[namespace]
	return snapshot, found
}

func TestResolver(t *testing.T) {
	base := Gates{Grove: true, DRA: true, Checkpoint: true}
	source := staticSnapshotSource{
		"tenant-a": `{"grove":false,"dra":false,"futureGate":true}`,
	}
	resolver := NewResolver(base, source.AdmissionGateSnapshot)
	resolved, warnings := resolver.ForNamespace("tenant-a")
	if resolved.Grove || resolved.DRA || !resolved.Checkpoint {
		t.Fatalf("namespaced false overrides were not applied: %#v", resolved)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v, want one unknown-gate warning", warnings)
	}
	resolved, warnings = resolver.ForNamespace("tenant-b")
	if resolved != base || len(warnings) != 0 {
		t.Fatalf("unclaimed namespace = %#v, %v, want %#v, no warnings", resolved, warnings, base)
	}
}

func TestResolveRejectsInvalidKnownValue(t *testing.T) {
	base := Gates{Grove: true}
	resolved, _, err := resolve(base, `{"grove":"yes"}`)
	if err == nil {
		t.Fatal("Resolve() should reject a non-boolean known gate")
	}
	if resolved != base {
		t.Fatalf("resolved gates = %#v, want base %#v", resolved, base)
	}
}

func TestGatesContext(t *testing.T) {
	want := Gates{GMSSnapshot: true}
	got := MustFromContext(WithGates(context.Background(), want))
	if got != want {
		t.Fatalf("MustFromContext() = %#v, want %#v", got, want)
	}
}

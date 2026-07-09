# Defaulting package

## Non-negotiable gate independence

**Defaulting must never depend on feature gates.**

- Do not read `features.Gates`, operator configuration booleans, environment
  variables, runtime capability detection, namespace Leases, or installation
  mode from a defaulter.
- A default may depend on the admitted object's API version and contents, but
  not on which operator instance or namespace handles the request.
- Keep defaults stable and deterministic across cluster-wide and
  namespace-restricted installations. Global admission must produce the same
  defaulted object for the same request regardless of feature configuration.
- Put gate-dependent acceptance policy in validation. Put gate-dependent
  materialization or repair in controller reconciliation.
- When a controller needs a value only for a gated execution path, let the
  controller fill it after admission instead of adding a conditional admission
  default.

The existing Grove-dependent `minAvailable` default is a legacy exception, not
a precedent. Do not extend it or copy its `GroveEnabled` pattern into new
defaulting behavior. Refactor or remove that exception separately when its
compatibility constraints allow it.

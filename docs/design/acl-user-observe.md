# Observe an existing ACL user without its password

Date: 2026-11-26. Status: Proposed. Ticket: RED-225344.

## TLDR

The ACL User CRD structurally requires a password Secret even for Observe-only management policies. Make its selector optional in the structural schema, while keeping the existing CEL requirement for Create and Update. Verify admission and actual managed observation without a password Secret in hosted CI.

## Behavior and scope

An existing ACL user may be declared Observe-only with its external identifier and no password Secret. This must not create, update or delete the remote user. Create, Update and the default wildcard policy continue to require the selector; a supplied selector still requires name, namespace and key. This does not weaken Terraform's own password requirement or provide an import credential bypass for mutation.

The Upjet generated sensitive selector is a value type even when its generated CEL condition is policy-dependent. The existing post-generation correction script changes both served API versions to pointers before controller-gen, making omitted selectors representable; regenerated deepcopy and CRD artifacts are committed. Go clients constructing these generated types must update selector assignments to pointers. No Terraform schema or provider code is modified.

## Verification and rollout

Hosted isolated Kubernetes API-server admission tests exercise both served versions: Observe without a Secret is accepted and read back through the other served version, Create/Update/default without one and incomplete selectors are rejected with password-specific validation, and Create/Update with a valid selector are accepted. A hosted loopback fixture exercises the actual managed reconciler and Terraform import/observe path without a password Secret and verifies no fake mutation. All tests are credential-free; no Cloud request or local acceptance environment is used. Fail closed if the Terraform provider cannot observe without a password.

Review generated diffs and schema regeneration, run exact-head hosted checks, and publish only via the existing release process. Roll back by reverting the API and generated artifact change; existing Observe-only objects without selectors would then be invalid on admission updates. Live QA and durable recovery remain separate requirements.

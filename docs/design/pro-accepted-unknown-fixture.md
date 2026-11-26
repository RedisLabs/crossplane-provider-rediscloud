# Pro database accepted-then-unavailable fixture

The managed Pro database lifecycle fixture exercises successful operations but
did not check the uncertain create outcome. The hosted fixture now separately
models fake Cloud accepting a create and returning HTTP 503 without a task ID.
The actual managed reconciler and pinned Terraform provider must report a failed
callback; subsequent reconciliations are drained and checked for replay. The
fixture requires one create, no delete, and successful authenticated loopback
reads of the fake parent and child. The existing successful lifecycle remains a
separate required case. The CI receipt explicitly requires both subtests.

This is a simulated, credential-free test, not a Cloud acceptance result. It
does not prove behavior across controller restarts, durable identity recovery,
shared-account fencing, or cleanup of a real resource. Real account writes
remain deferred until those separate safety gates are established.

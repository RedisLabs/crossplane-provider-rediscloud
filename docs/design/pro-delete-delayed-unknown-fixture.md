# Pro database pending delete with unavailable response

Hosted CI exercises the actual managed reconciler and pinned Terraform provider
against a loopback fake. The fake accepts a delete but continues to expose the
child and returns HTTP 503. Bounded subsequent reconciles must retain the CR
and finalizer, perform a new authenticated provider read showing the child is
still present, and avoid issuing a second delete. The fixture then makes the fake
child absent; a provider 404 read and finalizer completion are required while
the fake parent remains. The normal and immediate-absence cases remain
separately required.

This is a simulated single-process scenario, not live Cloud qualification,
proof of a task's accepted status, a lost transport response, durable fencing,
controller restart safety, or real cleanup.

# Pro database delete accepted-then-unavailable fixture

Hosted CI runs the actual managed reconciler and pinned Terraform provider
against a loopback fake which removes a Pro database and returns HTTP 503 for
the delete. The case requires a failed callback and retained CR/finalizer,
then checks the exact fake child is absent while the fake parent remains.
It resumes reconciliation and requires a new authenticated fake child-404 read
by the provider and finalizer completion with exactly one create and one
delete. Normal lifecycle and create-uncertainty cases remain
separately required.

This bounds only the injected outcome in one process with an immediately
absent fake child. It does not establish safety for delayed Cloud deletion,
lost transport, uncertain task state, controller restart, durable fencing,
real Cloud cleanup or live qualification.

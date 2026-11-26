# Pro subscription accepted-then-unavailable fixture

The hosted managed-reconciler fixture models a fake Pro subscription create
accepted by the service, with its bootstrap database created, followed by a
503 response without a task ID. The callback must fail, subsequent reconciles
are drained, and the fixture requires one create, no delete, and authenticated
loopback reads of the retained fake parent and bootstrap database. The normal
successful lifecycle remains a separately required hosted case.

This is simulated evidence only. It does not establish live Cloud behavior,
durable identity recovery, shared-account fencing, process-restart behavior or
real cleanup. No Cloud credential or live resource is used.

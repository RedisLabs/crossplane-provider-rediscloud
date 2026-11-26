# Pro delete restart reproducer on an isolated API server

This hosted-only test runs the existing pinned provider and actual managed
reconciler against loopback fake Cloud, while storing the Pro Database CR in
an isolated envtest API server and etcd. It checks API-server UID assignment
and stale-resourceVersion conflict. After a simulated accepted delete returns
503 while the child remains, it reconstructs the reconciler, callback and
Upjet workspace store against the same persisted CR and workspace files. The
test requires retained finalizer, no second delete while pending, and provider
reads before and after fake completion.

The current unfenced implementation is expected to fail the no-replay check.
This reproducer does not install a fence or authorize any Cloud write. It is
an in-process reconciler/store reconstruction, not a killed pod or OS process,
and preserves the workspace directory. API-server conflict behavior alone is
not proof of an actual guarded reservation, tamper resistance, cross-CR
coordination, credential isolation, durable task correlation or live cleanup.

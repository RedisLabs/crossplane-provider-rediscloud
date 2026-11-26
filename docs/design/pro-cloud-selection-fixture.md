# Pro subscription cloud selection fixture

The actual managed-reconciler and Terraform fixture runs separate GCP and AWS
Pro subscription lifecycles against a loopback fake service. For each, the
fake service validates the provider and region in the create request and
returns the same values on read; the existing lifecycle checks resource
identity, bootstrap cleanup, and deletion. CI explicitly requires both named
cases and uses the pinned Terraform/provider artifacts.

These are credential-free simulated tests, not evidence of real cloud-region
availability, live acceptance, cross-account fencing, or production cleanup.

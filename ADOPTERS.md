# `urunc` Adopters

This is a list of organizations that have publicly shared that they use
`urunc`. If your organization uses `urunc`, we would love to hear about it.
Adding yourself to this list helps the project grow and lets others learn from
your experience.

## Adding your organization

Open a pull request that adds a row to the table below. Please:

- only add an organization you represent. Do not add entries on behalf of
  others,
- keep the table in alphabetical order by organization name,
- pick one of the [adopter types](#adopter-types) listed below,
- briefly describe how you use `urunc` (e.g. which unikernels or VM/Sandbox
  monitors, Kubernetes or plain containerd, production or evaluation).

If you prefer not to open a PR, reach out to the maintainers on the
[Slack channel](https://cloud-native.slack.com/archives/C08V201G35J) or the
[maintainers' mailing list](mailto:cncf-urunc-maintainers@lists.cncf.io) and we
will add you.

## Adopters

| Organization | Type | Since | Use case |
| ------------ | ---- | ----- | -------- |
| [brig.sh](https://www.brig.sh/) (by NOFire AI) | Vendor | 2026 | A microVM-based sandbox for AI agents, available as open source and as a commercial offering, using `urunc` to create Linux-based sandboxes over cloud-hypervisor and hvi. |
| [NOFire AI](https://www.nofire.ai/) | End user | 2026 | All deployments of the NOFire AI service run over `urunc`, as Linux-based microVMs using cloud-hypervisor in the production Kubernetes cluster. |
<!--
Example entry:
| [Example Inc.](https://example.com) | End user | 2025 | Runs Unikraft unikernels with Firecracker as Kubernetes pods for multi-tenant serverless functions. |
-->

## Adopter types

- **End user**: An organization that uses `urunc` in its own infrastructure,
  in production or in an evaluation that is moving toward production.
- **Vendor**: An organization that ships `urunc` as part of a product or
  service offered to others.
- **Integration**: A project or product that integrates with `urunc` (e.g. a
  unikernel framework, a VM/Sandbox monitor, a platform or tool that deploys
  `urunc`).

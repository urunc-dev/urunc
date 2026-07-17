---
layout: default
title: "Security Policy"
description: "Security and Vulnerability Reporting"
---

Security is one of the main goals of `urunc`. If you discover a security issue,
we ask that you report it promptly and privately to the maintainers.  This page
provides information on when and how to report a vulnerability, along with the
description of the process of handling such reports.

## Security disclosure policy

The `urunc` project follows a [responsible disclosure
model](https://en.wikipedia.org/wiki/Coordinated_vulnerability_disclosure).
All vulnerability reports are reviewed by the `urunc` maintainers.  If
necessary, the report may be shared with other trusted contributors to aid in
finding a proper fix. 

## Reporting Vulnerabilities

Please do not open public issues or PRs to report a vulnerability. Instead, use
the private vulnerability reporting of `urunc`'s Github repository. In
particular, in `urunc`'s repository page, navigate to the [Security
tab](https://github.com/urunc-dev/urunc/security), click
[`Advisories`](https://github.com/urunc-dev/urunc/security/advisories) and then
[`Report a
vulnerability`](https://github.com/urunc-dev/urunc/security/advisories/new).
Alternatively, the report can be filed via email at `security@urunc.io`. This
address delivers your message securely to all maintainers.

### Vulnerability handling process

Upon the receipt of a vulnerability report, the following process will take place:

- the `urunc` maintainers will acknowledge and analyze the report within 48
  hours
- A timeline (embargo period) will be agreed upon with the reporter(s) to keep
  the vulnerability confidential until a fix is ready
- The maintainers will prioritize and begin addressing the issue. They may
  request additional details or involve trusted contributors to help resolve
  the problem securely
- Reporters are encouraged to participate in solution design or testing. The
  maintainers will keep them updated throughout the process
- At the end of the timeline: a) a proper fix will be merged, b) a new (patched)
  version of `urunc` will get released and c) a public advisory will get published,
  giving credits to the reporter(s), unless they prefer to remain anonymous

### What to include in the report

To help the maintainers assess and resolve the issue efficiently,
please use the following template:

```
## Title
_Short title describing the problem._

## Description

### Summary
_Short summary of the problem. Make the impact and severity as clear as possible. For example: Supplementary groups are not set up properly inside a container._

### Details
_Give all details on the vulnerability. Pointing to the incriminated source code is very helpful for the maintainer._

### PoC
_Complete instructions, including specific configuration details, to reproduce the vulnerability._

### Impact
_What kind of vulnerability is it? Who is impacted?_

## Affected Products

### Ecosystem
_Should be something related to Go, C, Github Actions etc._

### Package Name
_eg. github.com/urunc-dev/urunc_

### Affected Versions
_eg. < 0.5.0_

### Patched Versions
_eg. 0.5.1_

### Severity
_eg. Low, Critical etc._

```

Also, please use one report per vulnerability and try to keep in touch in
case the `urunc` maintainers require more information.

### Scope clarification

As a sandboxed container runtime, `urunc` makes use of VM or software based
monitors to spawn workloads. Therefore, before submitting a report, please
ensure the issue lies within `urunc` itself and not in the guest (uni)kernel or
the monitor. If the vulnerability is in those components, kindly report it to
their respective teams. However, if urunc uses those components in a way that
introduces a security issue, please report it to the urunc maintainers.

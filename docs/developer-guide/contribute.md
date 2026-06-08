---
layout: default
title: "Contributing"
description: "Contributing guidelines"
---

Urunc is an open-source project licensed under the [Apache License
2.0](https://github.com/urunc-dev/urunc/blob/main/LICENSE). We welcome anyone
who would be interested in contributing to `urunc`. As a first step, please
take a look at the following document and the [LLM
policy](../developer-guide/llm-policy.md).

## Table of contents:

1. [Code organization](#code-organization)
2. [How to contribute](#how-to-contribute)
3. [Opening an issue](#opening-an-issue)
4. [Requesting new features](#requesting-new-features)
5. [Submitting a PR](#submitting-a-pr)
6. [Style guide](#style-guide)
7. [Contact](#contact)

## Code organization

Urunc is written in Go and we structure the code and other files as follows:

- `/`: The root directory contains the Makefile to build `urunc`, along with other non-code files, such as the licence, readme and more.
- `/docs`: This directory contains all the documentation related to `urunc`, such as the installation guide, timestamping and more.
- `/cmd`: This directory contains handlers for the various command line options of `urunc` and the implementation of containerd-shim.
- `/internal/metrics`: This directory contains the implementation of the metrics logger, which is used for the internal measuring of `urunc`'s setup steps.
- `/pkg`: This directory contains the majority of the code for `urunc`. In particular, the subdirectory `/pkg/network/` contains network related code as expected, while the `/pkg/unikontainers/` subdirectory contains the main logic of `urunc`, along with the VMM/unikernel related logic.

Therefore, we expect any new documentation related files to be placed under `/docs` and any changes or new files in code to be either in the `/cmd/` or `/pkg/` directory.

## How to contribute

There are plenty of ways to contribute to an open source project, even without
changing or touching the code. Therefore, anyone who is interested in this
project is very welcome to contribute in one of the following ways:

- Using `urunc`. Try it out yourself and let us know your experience. Did
  everything work well? Were the instructions clear?
- Improve or suggest changes to the documentation of the project.
  Documentation is very important for every project, hence any ideas on how to
  improve the documentation to make it more clear are more than welcome.
- Request new features. Any proposals for improving or adding new features are
  very welcome.
- Find a bug and report it. Bugs are everywhere and some are hidden very well.
  As a result, we would really appreciate it if someone found a bug and
  reported it to the maintainers.
- Find an existing issue that interests you and try to resolve it. To get
  started, simply express your interest in a comment, and the maintainers will
  either assign the issue to you or ask for a proposal.

## Opening an issue

We use GitHub issues to track bugs and requests for new features. Anyone is
welcome to open a new issue, which is either related to a bug or a request for
a new feature. Please make sure to read the [LLM
policy](../developer-guide/llm-policy.md) in cases where an LLM has been used.

### Reporting bugs

In order to report a bug or misbehavior in `urunc`, a user can open a new issue explaining the problem.
For the time being, we do not use any strict template for reporting any issues.
However, in order to easily identify and fix the problem, it would be very helpful to provide enough information.
In that context, when opening a new issue regarding a bug, we kindly ask you to:

- Mark the issue with the bug label
- Provide the following information:

    1. A short description of the bug.
    2. The respective logs both from the output and containerd.
    3. Urunc's version (either the commit's hash or the version).
    4. The CPU architecture, VMM and the Unikernel framework used.
    5. Any particular steps to reproduce the issue.
- Keep an eye on the issue for possible questions from the maintainers.

A template for an issue could be the following one:
```
## Description
An explanation of the issue 

## System info

- Urunc version:
- Arch:
- VMM:
- Unikernel:

## Steps to reproduce
A list of steps that can reproduce the issue.
```

### Requesting new features

We will be very happy to listen to users about new features that they would like to see in `urunc`.
One way to communicate such a request is using GitHub issues.
For the time being, we do not use any strict template for requesting new features.
However, we kindly ask you to mark the issue with the enhancement label and provide a description of the new feature.

## Submitting a PR

Anyone should feel free to submit a change or an addition to the codebase of `urunc`.
Currently, we use GitHub's Pull Requests (PRs) to submit changes to `urunc`'s codebase.
Before creating a new PR, please follow the rules below:

- Avoid opening PRs for non-existent issues. Please create an issue first.
- Complete the PR template.
- In case LLMs have been used, please read the [LLM
  policy](../developer-guide/llm-policy.md).
- Avoid changes unrelated to the PR/issue.
- Test the changes locally.
- Make sure that the changes do not break the building process of `urunc`.
- Make sure that all the tests run successfully.
- Make sure that no commit in a PR breaks the building process of `urunc`.
- Make sure to sign-off your commits.
- Organize changes into logical commits. Each commit should represent a single,
  specific change, avoiding both overly large and overly small commits.
- Provide meaningful commit messages, describing shortly the changes.
- Provide a meaningful PR message.

The maintainers and admins of the `urunc` project reserve the right to close
PRs that do not comply with the above rules, with reference to this contribution
guide.

A new (draft) PR triggers the following process:

  1. A maintainer will check the PR and apply the `ok-to-test` label. This will
     trigger our CI to make sure the PR changes do not break existing
     functionality.
  2. The PR owner makes sure the CI tests pass, and marks the PR as ready to review.
  3. The reviewers submit their comments & reviews (if any).
  4. The PR owner should address comments (if any).
  5. If the PR is external (forked repo), a maintainer adds the `takeover`
     label, that triggers the change of the base branch to `main-pr<PRID>`. If
     the PR is internal (local branch) there is no need for the `takeover` label.
  6. As soon as a reviewer approves the PR, an action will be triggered to add
     the appropriate trailers in the PR's commits and merge the PR with the
     main branch. If the PR is external, the maintainer should rebase & merge for
     the action to be triggered. If the PR is internal, the action will be triggered
     automatically.

## Labels for the CI

We use GitHub workflows to invoke some tests when a new PR opens for `urunc`.
In particular, we perform the following workflows tests:

- Linting of the commit message. Please check the [git commit message style](#git-commit-messages) below for more info.
- Spell check, since `urunc` repository contains its documentation too.
- License check
- Code linting for Go.
- Building artifacts for amd64 and aarch64.
- Unit tests
- End-to-end tests

For a better control over the tests and workflows that run in a PR, we define
three labels which can be used:

- `ok-to-test`: Runs a full CI workflow, meaning all lint tests (commit
  message, spellcheck, license), Go's linting, building for x86 and aarch64,
  unit tests and at last end-to-end tests.
- `skip-build`: Skips the building workflows along with unit and end-to end tests
  running all the linting tests. This is useful when
  the PR is related to docs and it can help for catching spelling errors etc. In
  addition, if the changes are not related to the codebase, running the
  end-to-end tests is not required and saves some time.
- `skip-lint`: Skips the linting phase. This is particularly useful on draft
  PRs, when we want to just test the functionality of the code (either a bug
  fix, or a new feature) and defer the cleanup/polishing of commits, code, and
  docs to when the PR will be ready for review.
- `takeover`: Changes the base branch for the PR from `main` to `main-pr<PRID>`.
  This is to facilitate adding git trailers on the commit messages, to mention
  `Reviewers` and link to the original GitHub PR for reference.

**Note**: Both `skip-build` and `skip-lint` assume that the `ok-to-test` label
is added.

## Style guide

### Git commit messages

Please follow the below guidelines for your commit messages:

- Limit the first line to 72 characters or less.
- Limit all the other lines to 80 characters
- Follow the [Conventional Commits](https://www.conventionalcommits.org/)
  specification and, specifically, format the header as `<type>[optional scope]:
  <description>`, where `description` must not end with a fullstop and `type`
  can be one of:

  - *feat*: A new feature
  - *fix*: A bug fix
  - *docs*: Documentation only changes
  - *style*: Changes that do not affect the meaning of the code (white-space,
    formatting, missing semi-colons, etc)
  - *refactor*: A code change that neither fixes a bug nor adds a feature
  - *perf*: A code change that improves performance
  - *test*: Adding missing tests
  - *build*: Changes that affect the build system or external dependencies
    (example scopes: gulp, broccoli, npm)
  - *ci*: Changes to our CI configuration files and scripts (example scopes:
    Travis, Circle, BrowserStack, SauceLabs)
  - *chore*: Other changes that don't modify src or test files
  - *revert*: Reverts a previous commit
- In case the PR is associated with an issue, please refer to it, using the git trailer `Fixes: #Nr_issue`
- Always sign-off your commit message

### Golang code style

We follow gofmt's rules on formatting Go code. Therefore, we ask all contributors to do the same.
Go provides the `gofmt` tool, which can be used for formatting your code.

## Contact

We kindly invite everyone interested in `urunc` to join our
[Slack channel](https://cloud-native.slack.com/archives/C08V201G35J).
To directly communicate with the maintainers, feel free to drop an email At
[`urunc`'s maintainers' mailing list](cncf-urunc-maintainers@lists.cncf.io )

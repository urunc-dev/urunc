---
layout: default
title: "LLM policy"
description: "LLM usage policy in urunc"
---

As a CNCF Sandbox project, `urunc` adheres to the [Linux Foundation Generative
AI Policy](https://www.linuxfoundation.org/legal/generative-ai). All members of
the `urunc` community are therefore expected to read and comply with the above
policy. This document enhances the Linux Foundation policy specifically
for the `urunc` project.

Large Language Models (LLMs) are capable of analyzing and generating massive
amounts of code in a very short period of time. While this can be beneficial,
like many open source projects, `urunc` has experienced an increase in
contributions that exhibit low quality and clear signs of unreviewed LLM usage.
For that reason, the `urunc` project has decided to enforce some rules
regarding the use of LLM in its development process:

- LLM generated code must be treated in the same way as any other code.
- The use of LLMs for any contributions must be disclosed, including the exact
  model that was used.
- The user of LLM takes full responsibility of the generated output.
- All LLM-generated content must be reviewed by the contributor before opening
  issues or PRs, ensuring correctness, quality, and relevance.
- The use of LLMs to respond to comments in issues or PRs is not permitted.
  Contributors are expected to express their own thoughts and ideas.  Acting as
  a mediator between another user and an LLM is not helpful.
- LLMs may assist in code review but can not replace human reviews. Human
  reviewer guidance takes precedence over any LLM comments.

The maintainers and admins of the `urunc` project reserve the right to
close issues or PRs that do not comply with the above rules,
with reference to this policy.

## The rationale of the above rules

The `urunc` project can significantly benefit from the responsible use of LLMs
and this policy is not intended to prohibit their use. Instead, it aims to
protect the project from the misuse of such tools. LLM outputs are
probabilistic in nature and may introduce subtle bugs, outdated practices,
security risks, or incorrect assumptions. As such, any code or technical
contribution produced with the assistance of an LLMs must be carefully reviewed
and tested. Responsibility for the correctness and quality of LLM-generated
output lies solely with the contributor who uses the tool.

Do not forget that behind the `urunc` project there are humans who aim to
provide meaningful feedback and genuine assistance to the community.  However,
an influx of low-quality contributions and bug reports significantly reduces
their time and energy to properly review and assist other meaningful
contributions.  Nevertheless, `urunc` is a unique and novel project with
specific design choices and constraints that often require deeper explanation
and discussion. As a result, LLMs may not produce helpful or accurate
output for many `urunc`-specific use cases.

With this in mind, it would be way more beneficial to engage in discussions
with other people sharing genuine ideas, thoughts and concerns over an issue, PR
or in the Slack channel. There is no need to consult LLMs for every
interaction, on the contrary do not hesitate to ask other members. Let's help
each other learn and share ideas to improve the project.

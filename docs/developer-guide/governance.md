# Governance

`urunc` is dedicated to enable the deployment of unikernels and single
application kernels in cloud-native environments. This governance document
explains how the project is run.

- [Values](#values)
- [Roles](#roles)
- [Meetings](#meetings)
- [CNCF Resources](#cncf-resources)
- [Code of Conduct Enforcement](#code-of-conduct)
- [Security Response Team](#security-response-team)
- [Voting](#voting)
- [Modifications](#modifying-this-charter)

## Values

`urunc` and its leadership embrace the following values:

* Openness: Communication and decision-making happens in the open and is
  discoverable for future reference. As much as possible, all discussions and
  work take place in public forums and open repositories.

* Fairness: All stakeholders have the opportunity to provide feedback and
  submit contributions, which will be considered on their merits.

* Community over Product or Company: Sustaining and growing our community takes
  priority over shipping code or sponsors' organizational goals.  Each
  contributor participates in the project as an individual.

* Inclusivity: We innovate through different perspectives and skill sets, which
  can only be accomplished in a welcoming and respectful environment.

* Participation: Responsibilities within the project are earned through
  participation, and there is a clear path up the contributor ladder into
  leadership positions.

## Roles

There are several roles relevant to `urunc`'s governance:

### Contributor

A Contributor to `urunc` is someone who has contributed to the project (e.g. code,
docs, CI) within the last 12 months. Contributors have read only access to the
urunc repositories on GitHub.

### Maintainer

`urunc` Maintainers (as defined by the [urunc Maintainers
team](https://github.com/orgs/urunc-dev/teams/maintainers)) have write access
to the `urunc` repository on GitHub, which gives the ability to approve / merge /
close PRs, trigger the CI and manage / classify project issues. All pull
requests require review by a maintainer other than the one submitting it.
"Large" changes are encouraged to gather consensus from multiple maintainers and
interested community members. Maintainers are active Contributors and
participants in the project, collectively managing the project's resources and
contributors.

This privilege is granted with some expectation of responsibility: maintainers
are people who care about `urunc` and want to help it grow and improve. A
maintainer is not just someone who can make changes, but someone who has
demonstrated their ability to collaborate with the team, get the most
knowledgeable people to review code and docs, contribute high-quality code, and
follow through to fix issues (in code or tests).

A maintainer is a contributor to the project's success and a citizen helping
the project succeed.

The collective team of all Maintainers is known as the Maintainer Council, which
is the governing body for the project.

#### Becoming a Maintainer

To become a Maintainer you need to demonstrate the following:

  * commitment to the project:
    * actively participate in mettings, discussions, contributions, code and
      documentation reviews for at least 6 months,
    * perform reviews for at least 3 non-trivial pull requests,
    * contribute 3 non-trivial pull requests and have them merged,
  * ability to write quality code and/or documentation,
  * ability to collaborate with the team,
  * understanding of how the team works (policies, processes for testing and
    code review, etc),
  * understanding of the project's code base and coding / documentation
    style.

A new Maintainer must be proposed by an established Contributor and/or an
existing maintainer by sending a message to the [developer mailing
list](mailto:dev@urunc.io). A simple majority vote of existing Maintainers
approves the application. Maintainer nominations will be evaluated without
prejudice to employer or demographics.

Maintainers who are selected will be granted the necessary GitHub rights,
and invited to the [private maintainer mailing list](mailto:dev-priv@urunc.io).

#### Removing a Maintainer

Maintainers may resign at any time if they feel that they will not be able to
continue fulfilling their project duties.

Maintainers may also be removed after being inactive, failure to fulfill their
Maintainer responsibilities, violating the Code of Conduct, or other reasons.
Inactivity is defined as a period of very low or no activity in the project for
6 months or more, with no definite schedule to return to full Maintainer
activity.

A Maintainer may be removed at any time by a 2/3 vote of the remaining maintainers.

Depending on the reason for removal, a Maintainer may be converted to Emeritus
status. Emeritus Maintainers will still be consulted on some project matters,
and can be rapidly returned to Maintainer status if their availability changes.

### Admin

`urunc` Admins (as defined by the [urunc Admin
team](https://github.com/orgs/urunc-dev/teams/admins) have admin access to the
`urunc` repo, allowing them to do actions like, change the branch protection
rules for repositories, delete a repository and manage the access of others.
The Admin group is intentionally kept small, however, individuals can
be granted temporary admin access to carry out tasks, like creating a secret
that is used in a particular CI infrastructure.
The Admin list is reviewed and updated twice a year and typically contains:
- A subset of the maintainer team
- Optionally, some specific people that the Maintainers agree on adding for a
  specific purpose (e.g. to manage the CI)

## Meetings

Time zones permitting, Maintainers are expected to participate in the [public
developer
meeting](https://github.com/urunc-dev/urunc/?tab=readme-ov-file#community-and-meetings),
which occurs every last Wed of each month at 3pm UTC.

Maintainers will also have closed meetings in order to discuss security reports
or Code of Conduct violations. Such meetings should be scheduled by any
Maintainer on receipt of a security issue or CoC report. All current Maintainers
must be invited to such closed meetings, except for any Maintainer who is
accused of a CoC violation.

## CNCF Resources

Any Maintainer may suggest a request for CNCF resources, either in the [mailing
list](mailto:cncf-urunc-maintainers@lists.cncf.io), or during a meeting.  A
simple majority of Maintainers approves the request.  The Maintainers may also
choose to delegate working with the CNCF to non-Maintainer community members,
who will then be added to the [CNCF's Maintainer
List](https://github.com/cncf/foundation/blob/main/project-maintainers.csv) for
that purpose.

## Code of Conduct

[Code of Conduct](./Code-of-Conduct.md) violations by community members will be
discussed and resolved by the maintainers privately. If a Maintainer is
directly involved in the report, the Maintainers will instead designate two
Maintainers to work with the CNCF Code of Conduct Committee in resolving it.

## Security Response Team

The Maintainers will appoint a Security Response Team to handle security reports.
This committee may simply consist of the Maintainer Council themselves.  If this
responsibility is delegated, the Maintainers will appoint a team of at least two 
contributors to handle it. The Maintainers will review who is assigned to this
at least once a year.

The Security Response Team is responsible for handling all reports of security
holes and breaches according to the [security policy](./security.md).

## Voting

While most business in `urunc` is conducted by "[lazy consensus](https://community.apache.org/committers/lazyConsensus.html)", 
periodically the Maintainers may need to vote on specific actions or changes.
A vote can be taken on [the developer mailing list](mailto:dev@urunc.io) or
[the private Maintainer mailing list](mailto:dev-priv@urunc.io) for security or conduct matters.  
Votes may also be taken at [the developer meeting](./meetings.md). Any Maintainer may
demand a vote be taken.

Most votes require a simple majority of all Maintainers to succeed, except where
otherwise noted. Two-thirds majority votes mean at least two-thirds of all 
existing maintainers.

## Modifying this Charter

Changes to this Governance and its supporting documents may be approved by a
2/3 vote of the Maintainers.

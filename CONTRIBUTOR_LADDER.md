# Gatekeeper Contributor Ladder

---

* [Overview](#overview)
* [Roles](#roles)
* [Community Member](#community-member)
* [Contributor](#contributor)
  * [How to become a contributor](#how-to-become-a-contributor)
* [Maintainer](#maintainer)
  * [How to become a maintainer](#how-to-become-a-maintainer)
  * [Maintainer responsibilities](#maintainer-responsibilities)
* [Emeritus Process](#emeritus-process)
* [Inactivity](#inactivity)

---

## Overview

This document defines the contributor ladder for the Gatekeeper project and how to participate with the goal of moving from a user to a maintainer. The ladder provides a clear path for community growth and establishes trust through demonstrated competence, commitment, and alignment with project values.

As a policy controller for Kubernetes, Gatekeeper has a high impact on cluster security and workload availability. This places emphasis on reliability, security, and careful consideration of changes. Contributors are expected to demonstrate sound judgment and deep understanding of the implications of policy enforcement in Kubernetes environments.

## Roles

The Gatekeeper project recognizes the following contributor roles:

* **Community Member** - Anyone who participates in the community
* **Contributor** - Regular contributors with triage permissions  
* **Maintainer** - Trusted contributors with full repository permissions

## Community Member

Everyone is a community member! 🎉 You've read this far so you're already ahead. 

Here are some ideas for how you can be more involved and participate in the community:

* Comment on an issue that you're interested in
* Submit a pull request to fix an issue or improve documentation
* Report a bug or request a feature
* Join our [weekly community meetings](https://docs.google.com/document/d/1A1-Q-1OMw3QODs1wT6eqfLTagcGmgzAJAjJihiO3T48/edit)
* Come chat with us in [OPA Slack](https://slack.openpolicyagent.org/) in the [`#opa-gatekeeper`](https://openpolicyagent.slack.com/messages/CDTN970AX) channel
* Participate in [OPA Gatekeeper Community Discussions](https://github.com/open-policy-agent/community/discussions/categories/gatekeeper)
* Contribute policies to the [Gatekeeper Policy Library](https://github.com/open-policy-agent/gatekeeper-library)

All community members must follow our [Code of Conduct](CODE_OF_CONDUCT.md).

## Contributor

[Contributors](https://docs.github.com/en/organizations/managing-access-to-your-organizations-repositories/repository-roles-for-an-organization#permissions-for-each-role) have the following capabilities:

* Triage permissions on the Gatekeeper repository
* Can be assigned to issues and pull requests
* Can apply labels, milestones, and close/reopen issues
* Can request reviews and mark duplicate issues

Contributors are expected to:

* Follow the [Contributing Guide](CONTRIBUTING.md)
* Abide by the [Code of Conduct](CODE_OF_CONDUCT.md)
* Participate constructively in discussions and reviews

### How to become a contributor

To become a contributor, you should:

* **Demonstrate commitment**: Be active in the community for at least 2 months, showing consistent participation through:
  * Commenting on issues with thoughtful analysis and feedback
  * Reviewing pull requests with constructive suggestions
  * Participating in community meetings or discussions
  
* **Show technical competence**: Make meaningful contributions such as:
  * Successfully merging at least 5 pull requests (documentation, bug fixes, features, or tests)
  * Demonstrating understanding of Gatekeeper's architecture and OPA/Rego
  * Showing familiarity with Kubernetes concepts relevant to policy enforcement
  
* **Build trust**: Show good judgment in interactions and demonstrate alignment with project values:
  * Providing helpful and respectful feedback to other contributors
  * Following the contribution guidelines and processes
  * Understanding the impact of policy changes on Kubernetes workloads

* **Request contributor status**: Reach out to existing maintainers via:
  * A GitHub issue using the contributor request template (if available)
  * Discussion in the weekly community meeting
  * Direct message to a maintainer in Slack

The request should include links to your contributions and a brief explanation of your interest in the project.

## Maintainer

[Maintainers](https://docs.github.com/en/organizations/managing-access-to-your-organizations-repositories/repository-roles-for-an-organization#permissions-for-each-role) are trusted contributors with full repository access, including:

* Admin permissions on the Gatekeeper repository
* Ability to approve and merge pull requests
* Push access to protected branches
* Repository configuration permissions
* Release management capabilities

### Maintainer responsibilities

Maintainers have significant responsibilities beyond code contributions:

* **Code stewardship**: Review and approve pull requests with careful attention to:
  * Security implications of policy changes
  * Backward compatibility and breaking changes
  * Performance impact on Kubernetes clusters
  * Code quality and maintainability

* **Community leadership**: Help foster a welcoming and inclusive environment by:
  * Mentoring new contributors and helping them grow
  * Facilitating discussions and resolving conflicts
  * Upholding the Code of Conduct
  * Organizing and participating in community meetings

* **Project management**: Contribute to the project's direction through:
  * Triaging issues and managing the project roadmap
  * Participating in release planning and management
  * Making decisions about feature requests and architectural changes
  * Ensuring proper documentation of changes and decisions

* **Quality assurance**: Maintain high standards for the project by:
  * Ensuring comprehensive testing of changes
  * Following security best practices
  * Maintaining CI/CD infrastructure
  * Monitoring project health and addressing technical debt

### How to become a maintainer

Becoming a maintainer is a significant step that requires demonstrating sustained commitment and expertise. The current maintainers will evaluate candidates based on:

**Prerequisites:**
* Must be a contributor for at least 6 months
* Must be a member of the GitHub organization
* Demonstrated sustained activity and contributions to the project

**Technical expertise:**
* **Deep understanding of Gatekeeper**: Demonstrated knowledge of the codebase, architecture, and design patterns
* **Kubernetes expertise**: Strong understanding of Kubernetes concepts, especially admission controllers, webhooks, and cluster security
* **OPA/Rego proficiency**: Experience with Open Policy Agent and Rego policy language
* **Code quality**: History of high-quality contributions with attention to testing, documentation, and maintainability

**Demonstrated contributions:**
* **Significant feature development**: Led development of substantial features or improvements
* **Code review excellence**: Provided thoughtful, thorough reviews of others' pull requests (at least 20 substantial reviews)
* **Bug fixes and maintenance**: Addressed critical issues and contributed to project stability
* **Documentation and testing**: Improved project documentation and test coverage

**Community involvement:**
* **Active participation**: Regular attendance at community meetings and engagement in discussions
* **Mentorship**: Helped other contributors and demonstrated leadership qualities
* **Decision making**: Participated constructively in technical discussions and project decisions

**Process:**
1. **Self-nomination or nomination by existing maintainer**: Express interest in becoming a maintainer
2. **Maintainer discussion**: Current maintainers will discuss the candidate in private
3. **Evaluation period**: Candidate may be asked to demonstrate specific skills or take on additional responsibilities
4. **Consensus decision**: All current maintainers must agree on new maintainer additions
5. **Onboarding**: New maintainers receive access and guidance on their responsibilities

Maintainer status is not solely about volume of contributions but about trust, judgment, and commitment to the project's success.

## Emeritus Process

Life priorities and interests change, and that's perfectly normal. When maintainers need to step back from their responsibilities, we have an emeritus process that:

* Recognizes their contributions and keeps them connected to the project
* Allows for a graceful transition of responsibilities
* Provides a path back to active maintainership if circumstances change

**Moving to emeritus status:**
* Maintainers can request emeritus status at any time
* Maintainers may be moved to emeritus status due to extended inactivity (see [Inactivity](#inactivity))
* Emeritus maintainers retain recognition but lose administrative access

**Returning from emeritus:**
* Emeritus maintainers can request to return to active status
* Must demonstrate renewed commitment and current knowledge of the project
* Should be active in the community for at least 1 month before restoration
* Requires consensus from current active maintainers

## Inactivity

Active maintainership is important for project health and security. To maintain the project's momentum and ensure responsive decision-making:

**Measuring inactivity:**
* No meaningful contributions (code, reviews, or community participation) for 6 months
* No response to direct communication for 1 month
* Missing multiple consecutive community meetings without notice

**Addressing inactivity:**
* Maintainers will attempt to contact inactive maintainers
* If no response within 2 weeks, the maintainer may be moved to emeritus status
* This process protects both the project and the inactive maintainer's reputation

**Consequences:**
* Loss of maintainer privileges and access
* Transition to emeritus status with recognition of past contributions
* Opportunity to return when circumstances allow

---

This ladder is a living document that evolves with our project and community. If you have suggestions for improvement, please open an issue or bring it up in our community meetings.

Thank you for being part of the Gatekeeper community! 🚀
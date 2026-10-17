# Contributing to Agent Post

Agent Post currently contains planning documents and repository metadata. There is no runnable application, release, CLI, MCP server, or application test suite. Useful contributions at this stage include reviewing the design, correcting inconsistencies, improving examples, and making the documents easier to use.

## Propose a change

Describe the concrete problem, the proposed behavior, and its effect on existing requirements. Reference stable IDs from [docs/requirements.md](docs/requirements.md). Preserve those IDs when editing or reorganizing the document.

Separate confirmed user requirements from implementation proposals and example defaults. A proposed technology, packaging format, or configuration value must not become a confirmed product decision solely because it appears in an example.

Use an issue to describe a problem or discuss an alternative. A focused pull request can include the proposed change directly. Keep its scope small enough to review, and update related documents when changing a protocol or architecture decision.

## Prepare a pull request

Explain what changes for a reader or future implementer, why the change is needed, which requirement IDs it affects, and how you checked it. State any assumptions or unresolved decisions.

For documentation changes, verify relative links, examples, terminology, and consistency with the confirmed requirements. Report the checks actually performed. There are no application build or test commands to run at this stage; do not invent results or installation instructions.

The public [implementation plan](docs/implementation-plan.md) explains the design and implementation sequence. Do not turn it, the README, or another document into a duplicate progress checklist. Maintainer task status is recorded separately in the maintainer development log.

## Protect private information

Use fictional identifiers and placeholders in examples. Do not submit API keys, credentials, personal file paths, actual private host addresses, or confidential agent conversations. For security-sensitive concerns, follow [SECURITY.md](SECURITY.md) instead of posting exploit details publicly.

Agent-generated contributions should receive the same factual review as any other contribution. Verify claims and sources, and never report a tool run, benchmark, or compatibility test that did not happen.

Participation follows the [Code of Conduct](CODE_OF_CONDUCT.md). Contributions are made under the repository's [MIT license](LICENSE), which is the maintainer's initial planning choice.

---
name: context-compression
description: Compress the working context of a long-running agent task into a dense, lossless-for-decisions handoff summary. Use when a session is getting long, before handing work to another session or sub-agent, when resuming after a break, or when the user asks to summarize progress or write a handoff.
---

# Context Compression

Produce a summary from which a fresh agent could continue the task **without re-reading the conversation**.

## Keep (verbatim where exactness matters)

- **Goal**: the user's request in their words, plus any refinements and explicit constraints ("don't touch X", style rules, branch names).
- **Decisions made** and *why* — so they are not re-litigated.
- **Current state**: files changed (path + one-line what/why), commits/branches/PRs, commands that pass or fail now.
- **Exact identifiers**: paths, function names, error messages, IDs, URLs, versions, config values.
- **Open items**: next step, known bugs, unanswered questions to the user, assumptions not yet verified.
- **Dead ends**: approaches tried and why they failed (one line each).

## Drop

Raw tool output, file dumps already reflected in the repo, exploratory chatter, superseded plans, pleasantries.

## Format

```
## Goal
## Constraints
## Decisions (with reason)
## State (done / in progress)
## Key facts & identifiers
## Dead ends
## Next steps
## Open questions
```

Bullets, no prose padding. Target ≤ 10% of the original length. Before finishing, check: could someone resume and make the *same* next move I would? If not, add what's missing.

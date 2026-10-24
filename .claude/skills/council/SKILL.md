---
name: council
description: Run a structured multi-perspective council on a hard decision — several distinct expert viewpoints argue independently, critique each other, then converge on a recommendation with explicit trade-offs and dissent. Use for architecture choices, risky changes, strategy questions, or when the user asks for a council, debate, or second opinions.
---

# Council

## 1. Frame the question

Restate the decision in one sentence, the constraints (time, budget, team, existing system), and what "good" looks like. Gather facts from the codebase or sources first — the council argues over evidence, not guesses.

## 2. Seat 3–5 members

Pick perspectives that will genuinely disagree for this question, e.g.:

- **Pragmatist** — simplest thing that ships and can be maintained.
- **Skeptic / Red team** — how this fails, gets abused, or costs more than expected.
- **Domain expert** — security, performance, UX, ops, finance… whichever matters most here.
- **Long-term architect** — what this looks like in two years and at 10× scale.
- **User advocate** — what the end user actually experiences.

## 3. Independent positions

Each member writes, without seeing the others: recommendation, top 3 reasons, biggest risk, and what evidence would change their mind. (If sub-agents are available and the user asked for it, run each member as a separate agent for independence.)

## 4. Cross-examination

Each member challenges the strongest opposing argument once. Drop claims that don't survive; note new facts that need checking and check them.

## 5. Verdict

- **Recommendation** and the conditions under which it holds.
- **Trade-offs accepted.**
- **Dissent** — the strongest remaining objection, stated fairly.
- **Reversibility** — how to back out, and the signal that should trigger it.
- **Next concrete step.**

Keep the final output tight: a short table of positions, then the verdict.

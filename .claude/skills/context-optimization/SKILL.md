---
name: context-optimization
description: Improve context efficiency while working — read only what is needed, search before reading, delegate broad sweeps, avoid re-reading and huge outputs, and keep durable notes. Use when working in large codebases, long tasks, or when the user asks to reduce token/context usage.
---

# Context Optimization

## Reading

- **Search, then read.** Use grep/glob to locate symbols; read the relevant line range (offset/limit), not whole large files.
- Never re-read a file you just wrote or edited; trust the tool result.
- Skim structure first (`ls`, file outlines, `head`) before deep reads.
- Read lockfiles, generated code, minified bundles and fixtures only when the task is about them.

## Command output

- Limit output: `| head -50`, `--quiet`, `-q`, `git log --oneline -20`, `git diff --stat` before full diffs.
- Run tests with focused selection (`-k`, `-run`, single package) and failure-only output first.
- Redirect large logs to a file and grep them.

## Delegation

- Broad fan-out searches ("where is X used across the repo") → a read-only sub-agent that returns conclusions, not dumps — when sub-agents are available and appropriate.
- Independent tool calls in parallel, in one turn.

## Memory

- Keep a short running notes file for long tasks (goals, decisions, state) so progress survives compaction.
- Put stable project knowledge (build/test commands, conventions) in `CLAUDE.md` instead of rediscovering it each session.

## Communication

- Don't restate large code blocks back to the user; reference `path:line`.
- Summaries over transcripts; decisions over deliberation.

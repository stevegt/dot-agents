---
name: todo-stack
description: Inspect and update tracked Markdown task stacks with todo-stack. Use when choosing the next task, recording completion or a blocker, adding prerequisite work, reprioritizing items, validating stacks, or generating a prerequisite graph.
---

# Coordinate Work With Markdown Stacks

Use `todo-stack` for every stack mutation. Read
`references/todo-stack.md` from this skill when item syntax or command
behavior is needed. Stack state orders work; linked specifications and source
evidence retain authority for their subjects.

## Read Current State

1. Select a scope. Omit `--scope` for `default`.
2. Continue only when the scope contains all five stack files.
3. Run `todo-stack validate --scope <scope>` before relying on them.
4. Use `list --stack default` for complete runnable order and `peek` for stack
   top. File tail is top; item IDs do not imply priority.

## Change State

- Add work with `push`.
- Add a discovered prerequisite before resuming its dependent task.
- Reprioritize with same-stack `move --id`; use `--after-id` to place it after
  another item in processing order.
- Move accepted work to `completed.md` only after its acceptance checks pass.
- Move an external wait to `blocked.md` with `--reason` naming the event.
- Restore blocked work to `default.md` with `--reason` naming verified evidence.
- Move an item to `wontfix.md` only after the user or repository owner approves
  that exact item and disposition. Supply both `--reason` and `--approval`.
- Do not use `pop` for ordinary completion or abandonment. Do not use `clear`
  without direct approval for the exact destructive action.

When acting on an earlier read, supply the observed stack revision through
`--expect` and item revision through `--expect-item`.

## Finish

Run `validate` again. If the repository uses the generated prerequisite view,
run `graph --base <old.dot> --output <new.dot>` and review the result. The
helper never commits, sends messages, starts processes, or grants authority.

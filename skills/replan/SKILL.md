---
name: replan
description: Review and restate the current proposed plan as a complete replacement. Use in Plan mode when the user says "replan" or asks to review, reconsider, refine, and restate the plan.
---

# Replan

## Workflow

1. Locate the latest `<proposed_plan>` and every subsequent user decision or correction.
2. Review the plan for contradictions, missing decisions, stale assumptions, unnecessary scope, and weak acceptance criteria.  Ensure the plan covers all outstanding items that have been discussed and not yet persisted to disk.
3. Preserve locked decisions. Do not reopen them unless new evidence creates a real conflict.
4. Recheck repository or environment facts only when they may have changed or the review exposes an evidence gap.
5. If a high-impact preference is still unknown, ask one focused question and do not emit an incomplete replacement plan.
6. Otherwise, output exactly one complete replacement `<proposed_plan>` block that incorporates every correction.

## Review Checklist

- Keep the goal, success criteria, audience, scope, and constraints explicit.
- Make implementation choices, interfaces, data flow, failure behavior, and compatibility decisions complete.
- Include proportionate tests and concrete acceptance criteria.
- State assumptions and defaults that materially affect implementation.
- Remove duplication and detail that does not help an implementer act safely.

## Output Rules

- Clearly title the plan as a revised plan, with an integer version number that is one greater than the previous version of the same plan.
- Restate the whole plan, not a patch, delta, or commentary-only review.
- Keep an acceptable plan unchanged when review finds no substantive correction.
- Follow the active Plan-mode format and finalization rules, including the exact `<proposed_plan>` tags.
- Do not ask whether to proceed.

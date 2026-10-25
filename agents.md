# Agent Instructions

- Treat each directory below `skills/` as a self-contained skill.
- When a skill applies, read its complete `SKILL.md` before acting.
- Resolve `scripts/`, `references/`, and `assets/` relative to the skill that
  names them.
- Prefer a skill's deterministic program over reimplementing the same operation
  in an ad hoc shell command.
- Do not install or synchronize this repository into a live agent configuration
  unless the user explicitly requests that separate action.

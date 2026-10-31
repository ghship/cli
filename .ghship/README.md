# ghship project integration

Integration template version: 1.0.0

Shared skill: `~/.agents/skills/ghship/SKILL.md`. Each user's own home applies.
Project customization: `.ghship/skill.md` (user-owned; preserved by update/uninstall).
Run `ghship project skill --show --json` to inspect its declared scope.
Run `ghship init --json` for actual repository readiness and missing steps.
Run `ghship user list --json` for token status; valid does not prove repository access.

Project role bindings and credentials are stored outside Git in user configuration.
Workflow/CI/CODEOWNERS changes require a real reviewed integration PR.
Installation alone does not prove tests pass, native governance is configured,
or the product is delivered. Use actual CI and independent review evidence.

Maintenance: `ghship project update`, `ghship project uninstall`.
These create local changes requiring review/commit; remote rules/history stay.

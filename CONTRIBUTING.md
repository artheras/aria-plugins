# Add an Aria plugin

Use one stable, lowercase kebab-case identifier per capability: for example, `commerce`, `github`, or `research`. Names identify installed plugins; `displayName` controls branding. Do not publish placeholder plugins or rename identifiers for a display change.

1. Create `plugins/<name>/README.md` and explain the workflow, runtime, required credentials, side effects, and verified setup.
2. Add `.aria-plugin/plugin.json` using Commerce as the Aria package example. Add a minimal `.claude-plugin/plugin.json` with the same name/version for format reuse.
3. Add `skills/<skill-name>/SKILL.md` under the plugin. Use valid name/description frontmatter and concrete instructions. Skill folder names must be unique across this catalogue because the current Aria installer discovers one shared root directory. Optional `skill-policy.json` declarations do not replace host authorization.
4. Add an entry to the root `.claude-plugin/marketplace.json`. Its source is `./plugins/<name>` and its skills list is `["./skills/<skill-name>"]`. The paths resolve inside the plugin for Claude and against the generated root mirror for Aria.
5. For tool integrations, provide `mcp.json` for Aria and `.mcp.json` for Claude. Declare dependencies and configure credentials through the environment. Package Go/Rust executables separately from the workflow; installing a skill does not install executables.
6. Run `make catalog-sync`, review the canonical and generated content, then `make test` and `make build`. Commit the plugin, root mirror, catalogue, and integrity lock together.

Aria's integrity algorithm is SHA-256 over each sorted relative path, NUL, file bytes, NUL. The Go generator matches Python Path ordering and ignores the same cache files. Locks detect content changes against the committed catalogue; they are not publisher signatures.

Do not edit generated `skills/*` or `skills.lock.json` directly. Third-party plugins can get a separate reviewed `external_plugins/` area once actual integrations are submitted; the current validator accepts local `plugins/<name>` packages only.

Keep host-specific loaders in Aria Code. This repository distributes plugin content and reusable tools. Current Aria installations require separate MCP registration; command files and manifests alone do not register slash commands.

## Which copy to edit

`plugins/<name>/skills/` is the canonical source. `skills/<name>/` at the repository root is a **generated mirror** for the current Aria installer: never edit it by hand. Change the plugin copy, run `make catalog-sync`, and commit both together. If `make catalog-check` reports a changed hash or a stale mirror, you edited the mirror or forgot to sync.

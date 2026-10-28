# LinkedIn Content plugin — local prototype

A Python 3 local review calendar plus MCP stdio bridge. MCP exposes only `linkedin_create_draft` and `linkedin_list_posts`; approvals and queue actions are browser-only. It does not connect to LinkedIn or Buffer, publish, generate content on a schedule, or perform real retries. No credentials needed. SQLite data is stored next to app.py; the UI and MCP must use the same directory or `LINKEDIN_DB` environment variable.

Run `python3 app.py` and open http://127.0.0.1:8765. Add MCP server using `python3 /absolute/path/to/mcp_server.py` in your host's settings. `mcp.json` follows the Aria repository's example; `.mcp.json` is a Claude-style template, requiring LINKEDIN_PLUGIN_PATH to be set to the absolute directory. Host installation, environment interpolation and discovery have not been verified. The standalone skill can guide a connected agent; the model generates drafts, MCP stores them. Local UI is single-user and must not be exposed publicly.

Package follows the structure of artheras/aria-plugins, but is not registered in its marketplace or lock file and has not passed that repository's catalogue validation. Integration requires adding a marketplace entry, running `make catalog-sync`, `make test` and `make build` in a checkout, and verifying each host. Do not claim full platform compatibility until tested.

All edits reset approval. Approval does not publish. A real publisher must verify the exact approved account, content and scheduled time atomically, use authorized provider access, handle uncertain outcomes without blind retries, and implement persistent scheduling before production use.

## Daily scheduling core (prototype)

`scheduler.py` provides persistent local-time daily schedules, an atomic claim per schedule and local date, and run results marked `awaiting_review` or `failed`. A host must call `claim_due`, invoke its own model and image tools, then call `complete`. This module does not start a background worker, notify users, connect to a host conversation, generate content, approve or publish. Missed previous dates are skipped; when started after today's scheduled time, today's run is claimed. Expired claims are deliberately not retried automatically. SQLite is intended for a single local host; use a transactional shared store for distributed execution. The returned result is not yet integrated into the existing review calendar. DST ambiguous or nonexistent wall times have not been validated and need an explicit product policy before production use.

## System memory and files: requirement, not yet connected

The host should retrieve authorized user and organization memory and approved brand materials for each run. Keep personal and enterprise context scoped by authenticated owner and workspace; record which sources and versions were used without copying secrets into the draft. Use memory as context, not authorization to publish or proof that claims are true. Missing context must be surfaced for human input. Generated text, image assets and requested files should be reviewable as a versioned bundle, with every change invalidating approval. Actual host memory APIs, image-generation tools, file integration and provenance storage are not implemented in this prototype.

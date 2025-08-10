# Changelog

## v0.1.0 — 2025-08-10
- Initial preview release of `prezerolog`:
  - JSON logs to rotated files (lumberjack)
  - Colored console output in development/DEBUG
  - Normalized keys: `time, level, msg, service, env, trace_id, span_id, request_id, caller, err`
  - RFC3339Nano timestamps (UTC)
  - Context-aware correlation IDs
  - Fatal with stack (dev) and exit code
  - Package-level helpers (`prezerolog.Info`, `prezerolog.InfoCtx`, etc.)

# Changelog

## v0.3.0 - 2025-08-11
- Updated environmental variables to be specific to LOG_
- Updated example demo code
- Updated fatal code
- Updated Screenshots with env/json
- Updated README and CHANGELOG

## v0.2.0 — 2025-08-10
- Updated exit codes/fatal
- Working caller code

## v0.1.1 — 2025-08-10
- Updated caller code
- Updated README with screenshots

## v0.1.0 — 2025-08-10
- Initial preview release of `prezerolog`
- JSON logs to rotated files (lumberjack)
- Colored console output in development/DEBUG
- Normalized keys: `time, level, msg, service, env, trace_id, span_id, request_id, caller, err`
- RFC3339Nano timestamps (UTC)
- Context-aware correlation IDs
- Fatal with stack (dev) and exit code
- Package-level helpers (`prezerolog.Info`, `prezerolog.InfoCtx`, etc.)

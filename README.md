# Pre-zerolog (prezerolog)

A logging wrapper around [zerolog](https://github.com/rs/zerolog) with:
- JSON logs to rotated files
- Colored console output in development/DEBUG
- Normalized keys: `time, level, msg, service, env, trace_id, span_id, request_id, caller, err`
- RFC3339Nano timestamps (UTC)
- Context-aware correlation IDs
- Minimal API for consistency across services

## Install
```bash
go get github.com/renegadevi/prezerolog@v0.1.0


## Quick start
```go
package main

import (
  "context"
  log "github.com/renegadevi/prezerolog"
)

func main() {
  // One-liner init (loads .env if present, sets up rotation & console)
  log.InitLogging()

  // Plain
  log.Info("service starting")

  // Structured
  log.Info("login", map[string]any{"user":"alice", "role":"admin"})

  // With context (request/trace/span IDs)
  ctx := context.Background()
  ctx = context.WithValue(ctx, log.CtxRequestID, "req-7c1b")
  ctx = context.WithValue(ctx, log.CtxTraceID,   "tr-9a22")
  ctx = context.WithValue(ctx, log.CtxSpanID,    "sp-001")

  log.InfoCtx(ctx, "db connected", map[string]any{"dsn":"postgres://..."})
}
```

## Log files & rotation

By default, logs are written to `LOG_DIR` (default: `logs/`) as JSON.
Each run uses a date-based base name and a zero-padded index that increments on rotation.

**Fresh start:**
```
logs/
└─ my-service_2025-08-10T15-10-22_00.log
```

**After a few rotations:**
```
logs/
├─ my-service_2025-08-10T15-10-22_00.log
├─ my-service_2025-08-10T15-10-22_01.log
└─ my-service_2025-08-10T15-10-22_02.log
```

Where:
- `my-service_YYYY-MM-DDTHH-MM-SS` = base name for the current process start
- `_00.log` = the first file for this run; subsequent files increment to `_01.log`, `_02.log`, …

Rotation is controlled by environment variables:
- `LOG_ROTATE_MAX_SIZE` (MB, default `100`)
- `LOG_ROTATE_MAX_BACKUPS` (default `7`, older files are removed beyond this count)
- Files are compressed (`.gz`) automatically when rotated.

**Example JSON line:**
```json
{
  "time":"2025-08-10T13:45:15.123456789Z",
  "level":"info",
  "msg":"login",
  "service":"my-service",
  "env":"development",
  "trace_id":"tr-9a22",
  "span_id":"sp-001",
  "request_id":"req-7c1b",
  "caller":"auth.go:88",
  "user":"john",
  "role":"admin"
}
```

## Keys
All records include:

`time`, `level`, `msg`, `service`, `env`, `caller`

Optional correlation IDs (when using *Ctx funcs and a context carrying values):

`trace_id`, `span_id`, `request_id`

Errors passed as `error` args are logged under `err`.

## Environment variables
```ini
# When true: console pretty/color output is enabled
DEBUG=true

# Application environment: development | production
APP_ENV=development

# Logical service name shown in records (defaults to the binary name if unset).
SERVICE_NAME=my-service

# Directory where rotated JSON log files are written.
LOG_DIR=logs

# Max log file size in MB before rotation.
LOG_ROTATE_MAX_SIZE=100

# How many rotated files to keep.
LOG_ROTATE_MAX_BACKUPS=7

# Optional sampling (1 = no sampling; N>1 logs roughly 1 of every N records).
LOG_SAMPLING_N=1
```



## License
MIT 2025 - Philip Andersen
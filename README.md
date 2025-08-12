# Pre-zerolog (prezerolog)

[![Go Reference](https://pkg.go.dev/badge/github.com/renegadevi/prezerolog.svg)](https://pkg.go.dev/github.com/renegadevi/prezerolog)
[![License](https://img.shields.io/github/license/renegadevi/prezerolog)](https://github.com/renegadevi/prezerolog/blob/main/LICENSE)
[![Release](https://img.shields.io/github/v/release/renegadevi/prezerolog)](https://github.com/renegadevi/prezerolog/releases)


A logging wrapper around [zerolog](https://github.com/rs/zerolog) with:
- JSON logs to rotated files (optional to STDOUT for use to ingest)
- Colored console output in development/DEBUG
- Normalized keys: `time, level, msg, service, env, trace_id, span_id, request_id, caller, err`
- RFC3339Nano timestamps (UTC)
- Context-aware correlation IDs
- Minimal API for consistency across services

## Install
```bash
go get github.com/renegadevi/prezerolog
```

## Quick start
```go
package main

import (
	"context"
	"errors"

	log "github.com/renegadevi/prezerolog"
)

func main() {
	// Initialize logging (loads .env if present, sets up rotation & console)
	log.InitLogging()

	// Logging exmaples (check example file for more)
	log.Info("Logging initialized")
	log.Warn("This is a warning message")
	log.Error("This is an error message", errors.New("example error"))
	log.Trace("This is a trace message", map[string]any{"key": "value"})
	log.Debug("This is a debug message", map[string]any{"debug": true})

}

```


## Screenshots

![screenshot-env](https://raw.githubusercontent.com/renegadevi/prezerolog/main/.github/screenshot-env.png)
![screenshot-json](https://raw.githubusercontent.com/renegadevi/prezerolog/main/.github/screenshot-json.png)
![screenshot-file](https://raw.githubusercontent.com/renegadevi/prezerolog/main/.github/screenshot-file.png)


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
LOG_DIR=<folder name>                          # (default: "logs")
LOG_NAME=<service name>                        # (default: current dir)
LOG_ENV=production|development                 # (default: production)
LOG_FILE=true|false                            # (default: true)
LOG_CONSOLE=true|false                         # (default: true)
LOG_CONSOLE_LEVEL=trace|debug|info|warn        # (default: info)
LOG_FILE_LEVEL=trace|debug|info|warn           # (default: info)
LOG_CONSOLE_OUTPUT=minimal|full|extended|json  # (default: full)
LOG_SAMPLING_N=1                               # (default: 1)
LOG_ROTATE_MAX_SIZE=100                        # (default: 100)
LOG_ROTATE_MAX_BACKUPS=7                       # (default: 7)
```

## Example of Fatal error messages.
```go
// Generic failure (defaults to 1)
log.Fatal("unrecoverable error", err)
```


## License
MIT 2025 - Philip Andersen
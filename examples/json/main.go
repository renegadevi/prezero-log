// examples/json/main.go
package main

import (
	"context"
	"errors"
	"time"

	log "github.com/renegadevi/prezerolog"
)

func main() {
	log.InitLogging()

	log.Info("json mode: logging initialized", map[string]any{"version": "0.1.0", "feature": "stdout-json"})

	// Basic structured logs
	log.Trace("cache miss", map[string]any{"key": "user:42"})
	log.Debug("connecting to db", map[string]any{"dsn": "postgres://..."})
	log.Info("service starting", map[string]any{"port": 8080})
	log.Warn("slow query", map[string]any{"duration_ms": 1500})
	log.Error("failed to fetch widget", errors.New("upstream timeout"))

	// Contextual logs (request/trace/span ids)
	ctx := context.Background()
	ctx = context.WithValue(ctx, log.CtxRequestID, "req-7c1b")
	ctx = context.WithValue(ctx, log.CtxTraceID, "tr-9a22")
	ctx = context.WithValue(ctx, log.CtxSpanID, "sp-001")

	log.InfoCtx(ctx, "incoming request",
		map[string]any{"method": "GET", "path": "/api/v1/widgets/42"})

	time.Sleep(25 * time.Millisecond)
	log.DebugCtx(ctx, "loaded widget", map[string]any{"id": 42, "owner": "alice"})

	time.Sleep(10 * time.Millisecond)
	log.InfoCtx(ctx, "request complete",
		map[string]any{"status": 200, "duration_ms": 35})

	// Uncomment to see a fatal exit with code:
	// log.FatalCode(2, "fatal example: unrecoverable condition")
}
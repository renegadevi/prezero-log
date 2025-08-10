package main

import (
	"context"

	log "github.com/renegadevi/prezerolog"
)

func main() {
	log.InitLogging()

	log.Info("service starting")

	// Structured fields
	log.Info("login", map[string]any{"user": "alice", "role": "admin"})

	// With context IDs
	ctx := context.Background()
	ctx = context.WithValue(ctx, log.CtxTraceID, "tr-001")
	ctx = context.WithValue(ctx, log.CtxSpanID, "sp-abc")
	ctx = context.WithValue(ctx, log.CtxRequestID, "req-42")

	log.InfoCtx(ctx, "db connected", map[string]any{"dsn": "postgres://..."})
}

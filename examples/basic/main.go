package main

import (
	"context"
	"errors"

	log "github.com/renegadevi/prezerolog"
)

func main() {
	// Initialize logging (loads .env if present, sets up rotation & console)
	log.InitLogging()

	// Standard logging usage
	log.Info("Logging initialized")
	log.Warn("This is a warning message")
	log.Error("This is an error message", errors.New("example error"))
	log.Trace("This is a trace message", map[string]any{"key": "value"})
	log.Debug("This is a debug message", map[string]any{"debug": true})

	// Contextual logging
	ctx := context.Background()
	ctx = context.WithValue(ctx, log.CtxRequestID, "req-123")
	ctx = context.WithValue(ctx, log.CtxTraceID, "tr-abc")
	ctx = context.WithValue(ctx, log.CtxSpanID, "sp-xyz")
	log.TraceCtx(ctx, "trace with ctx", map[string]any{"step": "ctx"})
	log.InfoCtx(ctx, "info with ctx", map[string]any{"user": "alice"})

	// log.Fatal("This is a fatal message with code", 42) // Uncomment to test fatal logging with code

}

// Package prezerolog provides a fast, consistent logging wrapper around zerolog.
//
// Features:
// - JSON logs to rotated files (lumberjack)
// - Colored console output in development or when DEBUG=true
// - Normalized keys: time, level, msg, service, env, trace_id, span_id, request_id, caller, err
// - RFC3339Nano timestamps, forced UTC
// - Context-aware correlation IDs (trace_id, span_id, request_id)
// - Handles string (message), error, map[string]any, and struct args
// - Fatal logs with stack in dev and configurable exit code
// - Optional sampling via LOG_SAMPLING_N (1 = no sampling)
//
// License: MIT - 2025 Philip Andersen
package prezerolog

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"gopkg.in/natefinch/lumberjack.v2"
)

// ---------- Exit codes ----------

const (
	ExitSuccess           = 0  // No error
	ExitGeneral           = 1  // Catch-all for unspecified errors
	ExitFileNotFound      = 2  // ENOENT equivalent
	ExitInvalidInput      = 3  // EINVAL equivalent
	ExitIOFailure         = 4  // General I/O error
	ExitIconDecodeFailure = 50 // Invalid icon format
	ExitConfigParseError  = 51 // Bad config file
	ExitAuthFailure       = 52 // Authentication issues
)

// ---------- Public types ----------

type Logger struct {
	rotator    *RotatingLogger
	consoleOut bool
	mu         sync.Mutex
}

type RotatingLogger struct {
	*lumberjack.Logger
	baseName string
	index    int
	mu       sync.Mutex
}

type FatalError struct {
	Message string
	Code    int
	File    string
}

// Context keys for correlation identifiers.
type ctxKey string

const (
	CtxRequestID ctxKey = "request_id"
	CtxTraceID   ctxKey = "trace_id"
	CtxSpanID    ctxKey = "span_id"
)

var AppLogger *Logger

// ---------- Initialization ----------

func InitLogging() {
	_ = godotenv.Load() // ok if missing

	// Env config
	logDir := getEnv("LOG_DIR", "logs")
	debugMode := getEnv("DEBUG", "false") == "true"
	env := getEnv("APP_ENV", "production")

	rotator := NewRotatingLogger(logDir)
	configureZerolog(rotator, env, debugMode)

	AppLogger = &Logger{
		rotator:    rotator,
		consoleOut: env == "development" || debugMode,
	}
}

func NewRotatingLogger(logDir string) *RotatingLogger {
	// Use SERVICE_NAME (fallback to binary name) for file prefix
	service := getEnv("SERVICE_NAME", defaultServiceName())
	serviceSafe := sanitizeServiceName(service)

	baseName := fmt.Sprintf("%s_%s", serviceSafe, time.Now().Format("2006-01-02T15-04-05"))

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		// Logging may not be configured yet; fail fast.
		panic(fmt.Errorf("failed to create log directory %q: %w", logDir, err))
	}

	return &RotatingLogger{
		Logger: &lumberjack.Logger{
			Filename:   filepath.Join(logDir, fmt.Sprintf("%s_00.log", baseName)),
			MaxSize:    getEnvInt("LOG_ROTATE_MAX_SIZE", 100),
			MaxBackups: getEnvInt("LOG_ROTATE_MAX_BACKUPS", 7),
			Compress:   true,
		},
		baseName: baseName,
	}
}

func configureZerolog(rotator *RotatingLogger, env string, debugMode bool) {
	// Normalize keys and time
	zerolog.TimestampFieldName = "time"
	zerolog.LevelFieldName = "level"
	zerolog.MessageFieldName = "msg"
	zerolog.CallerFieldName = "caller"
	zerolog.ErrorFieldName = "err"

	zerolog.TimeFieldFormat = time.RFC3339Nano
	zerolog.TimestampFunc = func() time.Time { return time.Now().UTC() }
	zerolog.CallerMarshalFunc = func(pc uintptr, file string, line int) string {
		return fmt.Sprintf("%s:%d", filepath.Base(file), line)
	}

	// Level
	logLevel := zerolog.InfoLevel
	if debugMode {
		logLevel = zerolog.DebugLevel
	}

	// File writer (always JSON)
	writers := []io.Writer{rotator}

	// Console writer (pretty/color in dev or DEBUG)
	if env == "development" || debugMode {
		consoleWriter := zerolog.ConsoleWriter{
			Out:        os.Stderr,
			TimeFormat: "15:04:05",
			NoColor:    !debugMode, // force color only when DEBUG=true
		}
		writers = append(writers, &consoleWriter)
	}

	multi := zerolog.MultiLevelWriter(writers...)

	// Build base logger (optional sampling)
	logger := zerolog.New(multi)
	if n := getEnvInt("LOG_SAMPLING_N", 1); n > 1 {
		// zerolog v1.34.0 expects uint32 for N
		logger = logger.Sample(&zerolog.BasicSampler{N: uint32(n)})
	}

	// Base fields (NOTE: no caller here; we set caller per-event)
	service := getEnv("SERVICE_NAME", defaultServiceName())
	envVal := env

	log.Logger = logger.
		Level(logLevel).
		With().
		Timestamp().
		Str("service", service).
		Str("env", envVal).
		Logger()
}

// ---------- Rotator ----------

func (rl *RotatingLogger) Rotate() error {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	if err := rl.Logger.Rotate(); err != nil {
		return err
	}

	rl.index++
	rl.Filename = filepath.Join(
		filepath.Dir(rl.Filename),
		fmt.Sprintf("%s_%02d.log", rl.baseName, rl.index),
	)

	return nil
}

func (rl *RotatingLogger) Write(p []byte) (n int, err error) {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	return rl.Logger.Write(p)
}

// ---------- Public API ----------

func (l *Logger) Shutdown() {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.rotator != nil {
		_ = l.rotator.Close()
	}
}

func (l *Logger) GetCurrentLogFile() string {
	return l.rotator.Filename
}

func (l *Logger) Info(args ...interface{})  { l.logEvent(zerolog.InfoLevel, args...) }
func (l *Logger) Warn(args ...interface{})  { l.logEvent(zerolog.WarnLevel, args...) }
func (l *Logger) Error(args ...interface{}) { l.logEvent(zerolog.ErrorLevel, args...) }
func (l *Logger) Debug(args ...interface{}) { l.logEvent(zerolog.DebugLevel, args...) }

// Context-aware variants (attach request/trace/span IDs if present).
func (l *Logger) InfoCtx(ctx context.Context, args ...interface{})  { l.logEventCtx(ctx, zerolog.InfoLevel, args...) }
func (l *Logger) WarnCtx(ctx context.Context, args ...interface{})  { l.logEventCtx(ctx, zerolog.WarnLevel, args...) }
func (l *Logger) ErrorCtx(ctx context.Context, args ...interface{}) { l.logEventCtx(ctx, zerolog.ErrorLevel, args...) }
func (l *Logger) DebugCtx(ctx context.Context, args ...interface{}) { l.logEventCtx(ctx, zerolog.DebugLevel, args...) }

// Fatal logs an error, includes a stack trace in development, and exits.
// Pass FatalError{Message, Code} to control message and exit code.
func (l *Logger) Fatal(args ...interface{}) {
	message, fields, errVal := processLogArgs(args)

	if l.consoleOut {
		fields["stack"] = string(debug.Stack())
	}

	ev := log.Logger.Fatal()
	if c := userCaller(); c != "" {
		ev = ev.Str("caller", c)
	}
	ev = ev.Fields(fields)
	if errVal != nil {
		ev = ev.Err(errVal)
	}

	if message != "" {
		ev.Msg(message)
	} else {
		ev.Send()
	}

	code := ExitGeneral
	for _, a := range args {
		if fe, ok := a.(FatalError); ok && fe.Code != 0 {
			code = fe.Code
			break
		}
	}
	os.Exit(code)
}

// ---------- Internals ----------

func (l *Logger) logEvent(level zerolog.Level, args ...interface{}) {
	ev := log.WithLevel(level)
	if c := userCaller(); c != "" {
		ev = ev.Str("caller", c)
	}

	message, fields, errVal := processLogArgs(args)
	for k, v := range fields {
		ev = ev.Interface(k, v)
	}
	if errVal != nil {
		ev = ev.Err(errVal)
	}

	if message != "" {
		ev.Msg(message)
	} else {
		ev.Send()
	}
}

func (l *Logger) logEventCtx(ctx context.Context, level zerolog.Level, args ...interface{}) {
	// Attach correlation IDs from context.
	e := log.Logger.With()
	if v, _ := ctx.Value(CtxRequestID).(string); v != "" {
		e = e.Str("request_id", v)
	}
	if v, _ := ctx.Value(CtxTraceID).(string); v != "" {
		e = e.Str("trace_id", v)
	}
	if v, _ := ctx.Value(CtxSpanID).(string); v != "" {
		e = e.Str("span_id", v)
	}
	child := e.Logger()

	ev := child.WithLevel(level)
	if c := userCaller(); c != "" {
		ev = ev.Str("caller", c)
	}

	message, fields, errVal := processLogArgs(args)
	for k, v := range fields {
		ev = ev.Interface(k, v)
	}
	if errVal != nil {
		ev = ev.Err(errVal)
	}

	if message != "" {
		ev.Msg(message)
	} else {
		ev.Send()
	}
}

func processLogArgs(args []interface{}) (string, map[string]interface{}, error) {
	var message string
	fields := make(map[string]interface{})
	var errVal error

	for _, arg := range args {
		switch v := arg.(type) {
		case string:
			message = v // last string wins
		case error:
			errVal = v
		case map[string]interface{}:
			mergeMap(fields, v)
		case fmt.Stringer:
			// Helpful for types that implement String()
			if message == "" {
				message = v.String()
			} else {
				fields["value"] = v.String()
			}
		default:
			mergeStruct(fields, v)
		}
	}

	return message, fields, errVal
}

func mergeMap(target map[string]interface{}, source map[string]interface{}) {
	for k, v := range source {
		target[k] = v
	}
}

func mergeStruct(target map[string]interface{}, obj interface{}) {
	if obj == nil {
		return
	}
	rv := reflect.ValueOf(obj)
	if rv.Kind() == reflect.Pointer {
		if rv.IsNil() {
			return
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		// Fallback to raw value for non-structs
		target["value"] = obj
		return
	}
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" { // unexported
			continue
		}
		target[f.Name] = rv.Field(i).Interface()
	}
}

// ---------- Caller helpers ----------

// userCaller returns "file.go:line" for the first stack frame outside this package.
func userCaller() string {
	// Walk a reasonable number of frames upward
	for i := 2; i < 30; i++ {
		pc, file, line, ok := runtime.Caller(i)
		if !ok {
			break
		}
		if !isThisPackageFrame(pc, file) {
			return fmt.Sprintf("%s:%d", filepath.Base(file), line)
		}
	}
	return ""
}

func isThisPackageFrame(pc uintptr, file string) bool {
	// Check by function name (most robust across module paths)
	if f := runtime.FuncForPC(pc); f != nil {
		name := strings.ToLower(f.Name())
		// e.g., "github.com/renegadevi/prezerolog.(*Logger).Info"
		if strings.Contains(name, "/prezerolog.") || strings.Contains(name, ".prezerolog.") {
			return true
		}
	}
	// Fallback: check file path
	p := strings.ToLower(filepath.ToSlash(file))
	return strings.Contains(p, "/prezerolog/")
}

// ---------- Package-level shortcuts ----------

// ensure makes sure AppLogger exists
func ensure() *Logger {
	if AppLogger == nil {
		InitLogging()
	}
	return AppLogger
}

// Simple variants
func Info(args ...interface{})  { ensure().Info(args...) }
func Warn(args ...interface{})  { ensure().Warn(args...) }
func Error(args ...interface{}) { ensure().Error(args...) }
func Debug(args ...interface{}) { ensure().Debug(args...) }

// Context-aware variants
func InfoCtx(ctx context.Context, args ...interface{})  { ensure().InfoCtx(ctx, args...) }
func WarnCtx(ctx context.Context, args ...interface{})  { ensure().WarnCtx(ctx, args...) }
func ErrorCtx(ctx context.Context, args ...interface{}) { ensure().ErrorCtx(ctx, args...) }
func DebugCtx(ctx context.Context, args ...interface{}) { ensure().DebugCtx(ctx, args...) }

// ---------- Small helpers ----------

func getEnv(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) int {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func defaultServiceName() string {
	if len(os.Args) > 0 {
		return filepath.Base(os.Args[0])
	}
	return "app"
}

// sanitizeServiceName makes SERVICE_NAME safe to use in filenames.
func sanitizeServiceName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	// Replace spaces and path separators with '-'
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
	// Keep [a-z0-9._-]; replace others with '-'
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	out := strings.Trim(b.String(), "-_.")
	if out == "" {
		return "service"
	}
	return out
}

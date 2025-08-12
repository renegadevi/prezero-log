// Package prezerolog provides a fast, consistent logging wrapper around zerolog.
//
// Features:
// - JSON logs to rotated files (lumberjack)
// - Colored console output in development or when DEBUG=true
// - Normalized keys: time, level, msg, service, env, trace_id, span_id, request_id, caller, err
// - RFC3339Nano timestamps, forced UTC
// - Context-aware correlation IDs (trace_id, span_id, request_id)
// - Handles string (message), error, map[string]any, and struct args
// - Fatal logs with stack in dev and app-defined exit code (default 1)
// - Optional sampling via LOG_SAMPLING_N (1 = no sampling)
//
// License: MIT - 2025 Philip Andersen
//
// Env keys:
// LOG_DIR=<folder name>                         (default: "logs")
// LOG_NAME=<service name>                       (default: current dir)
// LOG_ENV=production|development                (default: production)
// LOG_FILE=true|false                           (default: true)
// LOG_CONSOLE=true|false                        (default: true)
// LOG_CONSOLE_LEVEL=trace|debug|info|warn       (default: info)
// LOG_FILE_LEVEL=trace|debug|info|warn          (default: info)
// LOG_CONSOLE_OUTPUT=minimal|full|extended|json (default: full)
// LOG_SAMPLING_N=1                              (default: 1)
// LOG_ROTATE_MAX_SIZE=100                       (default: 100)
// LOG_ROTATE_MAX_BACKUPS=7                      (default: 7)

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

// ---------- Public types ----------
type Logger struct {
	rotator    *RotatingLogger
	consoleOut bool
	mu         sync.Mutex
}

type RotatingLogger struct {
	*lumberjack.Logger
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

	logDir := getEnv("LOG_DIR", "logs")
	logEnv := getEnv("LOG_ENV", getEnv("APP_ENV", "production"))

	fileEnabled := getEnvBool("LOG_FILE", true)
	consoleEnabled := getEnvBool("LOG_CONSOLE", true)

	consoleLevel := parseLevel(getEnv("LOG_CONSOLE_LEVEL", "info"))
	if consoleLevel == zerolog.NoLevel {
		consoleLevel = zerolog.InfoLevel
	}
	fileLevel := parseLevel(getEnv("LOG_FILE_LEVEL", "info"))
	if fileLevel == zerolog.NoLevel {
		fileLevel = zerolog.InfoLevel
	}

	var rotator *RotatingLogger
	if fileEnabled {
		rotator = NewRotatingLogger(logDir)
	}

	configureZerolog(rotator, logEnv, fileEnabled, consoleEnabled, consoleLevel, fileLevel)

	AppLogger = &Logger{
		rotator:    rotator,
		consoleOut: consoleEnabled,
	}
}

func NewRotatingLogger(logDir string) *RotatingLogger {
	service := getEnv("LOG_NAME", defaultServiceName())
	serviceSafe := sanitizeServiceName(service)
	baseName := fmt.Sprintf("%s_%s", serviceSafe, time.Now().Format("2006-01-02T15-04-05"))

	if err := os.MkdirAll(logDir, 0o755); err != nil {
		panic(fmt.Errorf("failed to create log directory %q: %w", logDir, err))
	}

	return &RotatingLogger{
		Logger: &lumberjack.Logger{
			Filename:   filepath.Join(logDir, fmt.Sprintf("%s_00.log", baseName)),
			MaxSize:    getEnvInt("LOG_ROTATE_MAX_SIZE", 100),
			MaxBackups: getEnvInt("LOG_ROTATE_MAX_BACKUPS", 7),
			Compress:   true,
		},
	}
}

// configureZerolog builds the base logger with JSON-to-file and optional console.
func configureZerolog(rotator *RotatingLogger, logEnv string, fileEnabled, consoleEnabled bool, consoleLevel, fileLevel zerolog.Level) {
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

	service := getEnv("LOG_NAME", defaultServiceName())

	// Build destinations (0..N)
	var dests []levelDest

	// JSON file destination (optional)
	if fileEnabled && rotator != nil {
		dests = append(dests, levelDest{w: rotator, min: fileLevel})
	}

	// Console destination
	if consoleEnabled {
		switch strings.ToLower(strings.TrimSpace(getEnv("LOG_CONSOLE_OUTPUT", "full"))) {
		case "json":
			// Raw JSON to stdout (for containers/collectors)
			dests = append(dests, levelDest{w: os.Stdout, min: consoleLevel})
		case "minimal":
			// time, level, message only (no caller), hide env/service/ids
			cw := zerolog.ConsoleWriter{
				Out:        os.Stderr,
				TimeFormat: "15:04:05",
				NoColor:    false,
			}
			cw.PartsOrder = []string{
				zerolog.TimestampFieldName,
				zerolog.LevelFieldName,
				zerolog.MessageFieldName,
			}
			cw.FieldsExclude = []string{"service", "env", "trace_id", "span_id", "request_id"}
			dests = append(dests, levelDest{w: &cw, min: consoleLevel})
		case "extended":
			// time, level, caller, message, and all fields (no excludes)
			cw := zerolog.ConsoleWriter{
				Out:        os.Stderr,
				TimeFormat: "15:04:05",
				NoColor:    false,
			}
			cw.PartsOrder = []string{
				zerolog.TimestampFieldName,
				zerolog.LevelFieldName,
				zerolog.CallerFieldName,
				zerolog.MessageFieldName,
			}
			dests = append(dests, levelDest{w: &cw, min: consoleLevel})
		case "full":
			fallthrough
		default:
			// time, level, caller, message; hide env/service/ids; keep other fields
			cw := zerolog.ConsoleWriter{
				Out:        os.Stderr,
				TimeFormat: "15:04:05",
				NoColor:    false,
			}
			cw.PartsOrder = []string{
				zerolog.TimestampFieldName,
				zerolog.LevelFieldName,
				zerolog.CallerFieldName,
				zerolog.MessageFieldName,
			}
			cw.FieldsExclude = []string{"service", "env", "trace_id", "span_id", "request_id"}
			dests = append(dests, levelDest{w: &cw, min: consoleLevel})
		}
	}

	// Safety fallback: if nothing enabled, write JSON to stdout at info
	if len(dests) == 0 {
		dests = append(dests, levelDest{w: os.Stdout, min: zerolog.InfoLevel})
	}

	// Multi-destination writer
	w := multiLevelWriter{dests: dests}

	// Base logger uses lowest destination level so writers can filter independently
	minLevel := dests[0].min
	for _, d := range dests[1:] {
		if d.min < minLevel {
			minLevel = d.min
		}
	}

	logger := zerolog.New(w)
	if n := getEnvInt("LOG_SAMPLING_N", 1); n > 1 {
		logger = logger.Sample(&zerolog.BasicSampler{N: uint32(n)})
	}

	log.Logger = logger.
		Level(minLevel).
		With().
		Timestamp().
		Str("service", service).
		Str("env", logEnv).
		Logger()
}

// ----- multiLevelWriter: fan-out with per-destination min levels -----

type levelDest struct {
	w   io.Writer
	min zerolog.Level
}

type multiLevelWriter struct {
	dests []levelDest
}

func (m multiLevelWriter) Write(p []byte) (int, error) {
	for _, d := range m.dests {
		if d.w != nil {
			_, _ = d.w.Write(p)
		}
	}
	return len(p), nil
}

func (m multiLevelWriter) WriteLevel(level zerolog.Level, p []byte) (int, error) {
	for _, d := range m.dests {
		if d.w != nil && level >= d.min {
			_, _ = d.w.Write(p)
		}
	}
	return len(p), nil
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
	if l.rotator == nil || l.rotator.Logger == nil {
		return ""
	}
	return l.rotator.Filename
}

func (l *Logger) Trace(args ...interface{}) { l.logEvent(zerolog.TraceLevel, args...) }
func (l *Logger) Debug(args ...interface{}) { l.logEvent(zerolog.DebugLevel, args...) }
func (l *Logger) Info(args ...interface{})  { l.logEvent(zerolog.InfoLevel, args...) }
func (l *Logger) Warn(args ...interface{})  { l.logEvent(zerolog.WarnLevel, args...) }
func (l *Logger) Error(args ...interface{}) { l.logEvent(zerolog.ErrorLevel, args...) }

func (l *Logger) TraceCtx(ctx context.Context, args ...interface{}) {
	l.logEventCtx(ctx, zerolog.TraceLevel, args...)
}
func (l *Logger) DebugCtx(ctx context.Context, args ...interface{}) {
	l.logEventCtx(ctx, zerolog.DebugLevel, args...)
}
func (l *Logger) InfoCtx(ctx context.Context, args ...interface{}) {
	l.logEventCtx(ctx, zerolog.InfoLevel, args...)
}
func (l *Logger) WarnCtx(ctx context.Context, args ...interface{}) {
	l.logEventCtx(ctx, zerolog.WarnLevel, args...)
}
func (l *Logger) ErrorCtx(ctx context.Context, args ...interface{}) {
	l.logEventCtx(ctx, zerolog.ErrorLevel, args...)
}

// Fatal logs an error, includes a stack trace in development, and exits.
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

	// Default to generic failure unless caller specified a code
	code := 1
	for _, a := range args {
		if fe, ok := a.(FatalError); ok && fe.Code != 0 {
			code = fe.Code
			break
		}
	}
	os.Exit(code)
}

func FatalCode(code int, args ...interface{}) {
	args = append(args, FatalError{Code: code})
	ensure().Fatal(args...)
}

func Fatal(args ...interface{}) { ensure().Fatal(args...) }

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
			message = v
		case error:
			errVal = v
		case map[string]interface{}:
			mergeMap(fields, v)
		case fmt.Stringer:
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
		target["value"] = obj
		return
	}
	rt := rv.Type()
	for i := 0; i < rv.NumField(); i++ {
		f := rt.Field(i)
		if f.PkgPath != "" {
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
func Trace(args ...interface{}) { ensure().Trace(args...) }
func Debug(args ...interface{}) { ensure().Debug(args...) }
func Info(args ...interface{})  { ensure().Info(args...) }
func Warn(args ...interface{})  { ensure().Warn(args...) }
func Error(args ...interface{}) { ensure().Error(args...) }

// Context-aware variants
func TraceCtx(ctx context.Context, args ...interface{}) { ensure().TraceCtx(ctx, args...) }
func DebugCtx(ctx context.Context, args ...interface{}) { ensure().DebugCtx(ctx, args...) }
func InfoCtx(ctx context.Context, args ...interface{})  { ensure().InfoCtx(ctx, args...) }
func WarnCtx(ctx context.Context, args ...interface{})  { ensure().WarnCtx(ctx, args...) }
func ErrorCtx(ctx context.Context, args ...interface{}) { ensure().ErrorCtx(ctx, args...) }

// ---------- Helpers functions ----------

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

func getEnvBool(key string, def bool) bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv(key)))
	if v == "" {
		return def
	}
	switch v {
	case "1", "t", "true", "y", "yes", "on":
		return true
	case "0", "f", "false", "n", "no", "off":
		return false
	default:
		return def
	}
}

func parseLevel(s string) zerolog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "trace":
		return zerolog.TraceLevel
	case "debug":
		return zerolog.DebugLevel
	case "info", "":
		return zerolog.InfoLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	case "fatal":
		return zerolog.FatalLevel
	case "panic":
		return zerolog.PanicLevel
	default:
		return zerolog.NoLevel
	}
}

func defaultServiceName() string {
	if bi, ok := debug.ReadBuildInfo(); ok && bi != nil && bi.Main.Path != "" {
		if last := filepath.Base(bi.Main.Path); last != "" && last != "." && last != "/" {
			return last
		}
	}
	if cwd, err := os.Getwd(); err == nil && cwd != "" {
		if base := filepath.Base(cwd); base != "" {
			return base
		}
	}
	if len(os.Args) > 0 {
		return filepath.Base(os.Args[0])
	}
	return "app"
}

func sanitizeServiceName(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, " ", "-")
	s = strings.ReplaceAll(s, "/", "-")
	s = strings.ReplaceAll(s, "\\", "-")
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

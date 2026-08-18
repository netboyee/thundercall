package logging

import (
	"log"
	"strings"
	"sync/atomic"
)

type Level int32

const (
	LevelDebug Level = iota
	LevelInfo
	LevelWarn
	LevelError
)

var currentLevel atomic.Int32

func init() {
	currentLevel.Store(int32(LevelInfo))
}

type Logger struct {
	component string
}

func New(component string) Logger {
	return Logger{component: strings.TrimSpace(component)}
}

func Configure(raw string) {
	currentLevel.Store(int32(ParseLevel(raw)))
}

func ParseLevel(raw string) Level {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "debug":
		return LevelDebug
	case "warn", "warning":
		return LevelWarn
	case "error":
		return LevelError
	default:
		return LevelInfo
	}
}

func Enabled(level Level) bool {
	return level >= Level(currentLevel.Load())
}

func (l Logger) DebugEnabled() bool {
	return Enabled(LevelDebug)
}

func (l Logger) Debugf(format string, args ...any) {
	l.printf(LevelDebug, format, args...)
}

func (l Logger) Infof(format string, args ...any) {
	l.printf(LevelInfo, format, args...)
}

func (l Logger) Warnf(format string, args ...any) {
	l.printf(LevelWarn, format, args...)
}

func (l Logger) Errorf(format string, args ...any) {
	l.printf(LevelError, format, args...)
}

func (l Logger) printf(level Level, format string, args ...any) {
	if !Enabled(level) {
		return
	}

	prefix := "level=" + level.String()
	if l.component != "" {
		prefix += " component=" + l.component
	}

	if strings.TrimSpace(format) == "" {
		log.Print(prefix)
		return
	}

	log.Printf(prefix+" "+format, args...)
}

func (l Level) String() string {
	switch l {
	case LevelDebug:
		return "DEBUG"
	case LevelWarn:
		return "WARN"
	case LevelError:
		return "ERROR"
	default:
		return "INFO"
	}
}

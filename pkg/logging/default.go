package logging

import (
	"os"
	"strings"
	"sync"
)

var (
	defaultOnce sync.Once
	defaultLog  *Logger

	debugOnce    sync.Once
	debugEnabled bool
)

func Default() *Logger {
	defaultOnce.Do(func() {
		l, err := New(projectRoot(), Options{DebugEnabled: DebugEnabled()})
		if err != nil {
			// Fall back to a disabled logger rather than failing the command.
			defaultLog = &Logger{debugEnabled: DebugEnabled()}
			return
		}
		defaultLog = l
	})
	return defaultLog
}

func envDebugEnabled() bool {
	v := strings.TrimSpace(os.Getenv("SUBTASK_DEBUG"))
	if v == "" || v == "0" {
		return false
	}
	v = strings.ToLower(v)
	return v == "1" || v == "true" || v == "yes"
}

func LogPath() string { return Default().Path() }

func DebugEnabled() bool {
	debugOnce.Do(func() {
		debugEnabled = envDebugEnabled()
	})
	return debugEnabled
}

func Debug(op, msg string) { Default().Debug(op, msg) }

func Info(op, msg string) { Default().Info(op, msg) }

func Error(op, msg string) { Default().Error(op, msg) }

func DebugTimer(op, startMsg string) func(doneMsg string) {
	return Default().DebugTimer(op, startMsg)
}

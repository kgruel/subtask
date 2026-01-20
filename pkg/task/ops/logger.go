package ops

// Logger is an optional callback sink for long-running operations.
// Implementations must be safe to call from the current goroutine.
type Logger interface {
	Info(msg string)
	Warning(msg string)
	Success(msg string)
}

func logInfo(l Logger, msg string) {
	if l != nil {
		l.Info(msg)
	}
}

func logWarning(l Logger, msg string) {
	if l != nil {
		l.Warning(msg)
	}
}

func logSuccess(l Logger, msg string) {
	if l != nil {
		l.Success(msg)
	}
}

package asyncify

import (
	"sync"

	"go.uber.org/zap"
)

var (
	logger     *zap.Logger
	loggerOnce sync.Once
)

// Logger returns the asyncify package's logger instance.
// It uses a no-op logger by default.
func Logger() *zap.Logger {
	loggerOnce.Do(func() {
		if logger == nil {
			logger = zap.NewNop()
		}
	})
	return logger
}

// SetLogger configures the asyncify package's logger.
// This must be called before any transformation.
func SetLogger(l *zap.Logger) {
	logger = l
}

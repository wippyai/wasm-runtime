package engine

import (
	"sync"

	"go.uber.org/zap"
)

var (
	logger     *zap.Logger
	loggerOnce sync.Once
)

// Logger returns the engine's logger instance.
// It uses a no-op logger by default.
func Logger() *zap.Logger {
	loggerOnce.Do(func() {
		if logger == nil {
			logger = zap.NewNop()
		}
	})
	return logger
}

// debugf is a no-op debug helper. Enable by setting debug = true.
var debug = false

func debugf(format string, args ...any) {
	if debug {
		Logger().Sugar().Debugf(format, args...)
	}
}

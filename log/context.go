package log

// @see https://github.com/containerd/containerd/blob/master/log/context.go

import (
	"context"
	"fmt"

	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

const (
	loggerKey = "logger"
)

var (
	defaultLogger = logrus.NewEntry(logrus.StandardLogger())
)

func WithLogger(ctx context.Context, logger *logrus.Entry) context.Context {
	return context.WithValue(ctx, "logger", logger)
}

func WithDefaultLogger(ctx context.Context) context.Context {
	return context.WithValue(ctx, "logger", defaultLogger)
}

func GetLogger(ctx context.Context) *logrus.Entry {
	logger := ctx.Value(loggerKey)

	if logger == nil {
		return defaultLogger
	}

	return logger.(*logrus.Entry)
}

func ConfigureDefaultLogger(level string) error {
	switch level {
	case "debug":
		logrus.SetLevel(logrus.DebugLevel)
	case "":
		fallthrough
	case "info":
		logrus.SetLevel(logrus.InfoLevel)
	case "warn":
		logrus.SetLevel(logrus.WarnLevel)
	case "error":
		logrus.SetLevel(logrus.ErrorLevel)
	case "fatal":
		logrus.SetLevel(logrus.FatalLevel)
	case "panic":
		logrus.SetLevel(logrus.PanicLevel)
	default:
		return errors.WithStack(errors.New(fmt.Sprintf("Invalid log level %q. Should be one of: debug, info, warn, error, fatal or panic.", level)))
	}

	return nil
}

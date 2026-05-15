package raftcluster

import (
	"io"
	"log"

	"github.com/hashicorp/go-hclog"
	"go.uber.org/zap"
)

// newSnapshotHclogLogger adapts Hashicorp FileSnapshotStore diagnostics to zap.
func newSnapshotHclogLogger(logger *zap.Logger) hclog.Logger {
	if logger == nil {
		return hclog.NewNullLogger()
	}

	return &snapshotHclogLogger{
		logger: logger.Named("snapshot"),
		name:   "snapshot",
	}
}

type snapshotHclogLogger struct {
	logger *zap.Logger
	name   string
}

func (l *snapshotHclogLogger) Log(level hclog.Level, msg string, args ...interface{}) {
	fields := hclogKeyvalsToZap(args...)
	switch level {
	case hclog.Error:
		l.logger.Error(snapshotLogMessage(msg), fields...)
	case hclog.Warn:
		l.logger.Warn(snapshotLogMessage(msg), fields...)
	case hclog.Info:
		l.logger.Info(snapshotLogMessage(msg), fields...)
	default:
		l.logger.Debug(snapshotLogMessage(msg), fields...)
	}
}

func (l *snapshotHclogLogger) Trace(msg string, args ...interface{}) {
	l.logger.Debug(snapshotLogMessage(msg), hclogKeyvalsToZap(args...)...)
}

func (l *snapshotHclogLogger) Debug(msg string, args ...interface{}) {
	l.logger.Debug(snapshotLogMessage(msg), hclogKeyvalsToZap(args...)...)
}

func (l *snapshotHclogLogger) Info(msg string, args ...interface{}) {
	l.logger.Info(snapshotLogMessage(msg), hclogKeyvalsToZap(args...)...)
}

func (l *snapshotHclogLogger) Warn(msg string, args ...interface{}) {
	l.logger.Warn(snapshotLogMessage(msg), hclogKeyvalsToZap(args...)...)
}

func (l *snapshotHclogLogger) Error(msg string, args ...interface{}) {
	l.logger.Error(snapshotLogMessage(msg), hclogKeyvalsToZap(args...)...)
}

func (l *snapshotHclogLogger) IsTrace() bool { return false }
func (l *snapshotHclogLogger) IsDebug() bool { return false }
func (l *snapshotHclogLogger) IsInfo() bool  { return true }
func (l *snapshotHclogLogger) IsWarn() bool  { return true }
func (l *snapshotHclogLogger) IsError() bool { return true }

func (l *snapshotHclogLogger) ImpliedArgs() []interface{}       { return nil }
func (l *snapshotHclogLogger) With(...interface{}) hclog.Logger { return l }
func (l *snapshotHclogLogger) Name() string                     { return l.name }

func (l *snapshotHclogLogger) Named(name string) hclog.Logger {
	return &snapshotHclogLogger{logger: l.logger.Named(name), name: name}
}

func (l *snapshotHclogLogger) ResetNamed(name string) hclog.Logger {
	return &snapshotHclogLogger{logger: l.logger.Named(name), name: name}
}
func (l *snapshotHclogLogger) SetLevel(_ hclog.Level) {}
func (l *snapshotHclogLogger) GetLevel() hclog.Level  { return hclog.Info }
func (l *snapshotHclogLogger) StandardLogger(_ *hclog.StandardLoggerOptions) *log.Logger {
	return log.New(io.Discard, "", 0)
}

func (l *snapshotHclogLogger) StandardWriter(_ *hclog.StandardLoggerOptions) io.Writer {
	return io.Discard
}

func snapshotLogMessage(msg string) string {
	switch msg {
	case "creating new snapshot":
		return "Raft snapshot created"
	case "reaping snapshot":
		return "Raft snapshot compacted"
	default:
		return msg
	}
}

func hclogKeyvalsToZap(args ...interface{}) []zap.Field {
	if len(args) == 0 {
		return nil
	}

	fields := make([]zap.Field, 0, len(args)/2+1)
	for i := 0; i+1 < len(args); i += 2 {
		key, ok := args[i].(string)
		if !ok {
			continue
		}
		switch key {
		case "error", "err":
			if err, ok := args[i+1].(error); ok {
				fields = append(fields, zap.Error(err))
				continue
			}
		case "path":
			if path, ok := args[i+1].(string); ok {
				fields = append(fields, zap.String("snapshot_path", path))
				continue
			}
		}
		fields = append(fields, zap.Any(key, args[i+1]))
	}

	return fields
}

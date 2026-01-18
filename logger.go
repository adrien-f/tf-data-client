package tfclient

import (
	"io"
	"log"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-hclog"
)

func newHclogAdapter(logger logr.Logger) hclog.Logger {
	return &hclogAdapter{logger: logger}
}

type hclogAdapter struct {
	logger      logr.Logger
	impliedArgs []interface{}
	name        string
}

func (a *hclogAdapter) Log(level hclog.Level, msg string, args ...interface{}) {
	switch level {
	case hclog.Trace, hclog.Debug:
		a.logger.V(1).Info(msg, args...)
	case hclog.Info:
		a.logger.Info(msg, args...)
	case hclog.Warn:
		a.logger.V(0).Info(msg, args...)
	case hclog.Error:
		a.logger.Error(nil, msg, args...)
	}
}

func (a *hclogAdapter) Trace(msg string, args ...interface{}) {
	a.logger.V(1).Info(msg, args...)
}

func (a *hclogAdapter) Debug(msg string, args ...interface{}) {
	a.logger.V(1).Info(msg, args...)
}

func (a *hclogAdapter) Info(msg string, args ...interface{}) {
	a.logger.Info(msg, args...)
}

func (a *hclogAdapter) Warn(msg string, args ...interface{}) {
	a.logger.V(0).Info(msg, args...)
}

func (a *hclogAdapter) Error(msg string, args ...interface{}) {
	a.logger.Error(nil, msg, args...)
}

func (a *hclogAdapter) IsTrace() bool {
	return a.logger.V(1).Enabled()
}

func (a *hclogAdapter) IsDebug() bool {
	return a.logger.V(1).Enabled()
}

func (a *hclogAdapter) IsInfo() bool {
	return a.logger.Enabled()
}

func (a *hclogAdapter) IsWarn() bool {
	return a.logger.Enabled()
}

func (a *hclogAdapter) IsError() bool {
	return a.logger.Enabled()
}

func (a *hclogAdapter) ImpliedArgs() []interface{} {
	return a.impliedArgs
}

func (a *hclogAdapter) With(args ...interface{}) hclog.Logger {
	return &hclogAdapter{
		logger:      a.logger.WithValues(args...),
		impliedArgs: append(a.impliedArgs, args...),
		name:        a.name,
	}
}

func (a *hclogAdapter) Name() string {
	return a.name
}

func (a *hclogAdapter) Named(name string) hclog.Logger {
	newName := name
	if a.name != "" {
		newName = a.name + "." + name
	}
	return &hclogAdapter{
		logger:      a.logger.WithName(name),
		impliedArgs: a.impliedArgs,
		name:        newName,
	}
}

func (a *hclogAdapter) ResetNamed(name string) hclog.Logger {
	return &hclogAdapter{
		logger:      a.logger.WithName(name),
		impliedArgs: a.impliedArgs,
		name:        name,
	}
}

func (a *hclogAdapter) SetLevel(level hclog.Level) {
	// logr doesn't support runtime level changes
}

func (a *hclogAdapter) GetLevel() hclog.Level {
	if a.logger.V(1).Enabled() {
		return hclog.Debug
	}
	return hclog.Info
}

func (a *hclogAdapter) StandardLogger(opts *hclog.StandardLoggerOptions) *log.Logger {
	return log.New(a.StandardWriter(opts), "", 0)
}

func (a *hclogAdapter) StandardWriter(opts *hclog.StandardLoggerOptions) io.Writer {
	return &hclogWriter{adapter: a}
}

type hclogWriter struct {
	adapter *hclogAdapter
}

func (w *hclogWriter) Write(p []byte) (n int, err error) {
	w.adapter.Info(string(p))
	return len(p), nil
}

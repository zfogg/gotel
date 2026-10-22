// Package slogadapter bridges common printf-style SDK logger interfaces to slog.
package slogadapter

import (
	"fmt"
	"log/slog"
)

type Formatted struct{ Logger *slog.Logger }

func (l Formatted) logger() *slog.Logger {
	if l.Logger != nil {
		return l.Logger
	}
	return slog.Default()
}
func (l Formatted) Infof(format string, args ...any)  { l.logger().Info(fmt.Sprintf(format, args...)) }
func (l Formatted) Warnf(format string, args ...any)  { l.logger().Warn(fmt.Sprintf(format, args...)) }
func (l Formatted) Errorf(format string, args ...any) { l.logger().Error(fmt.Sprintf(format, args...)) }

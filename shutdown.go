package gotel

import (
	"context"
	"errors"
)

// Shutdown joins provider shutdown functions and runs them in reverse order so
// the last configured pipeline is flushed first.
func Shutdown(shutdowns ...func(context.Context) error) func(context.Context) error {
	return func(ctx context.Context) error {
		var errs []error
		for i := len(shutdowns) - 1; i >= 0; i-- {
			if shutdowns[i] != nil {
				errs = append(errs, shutdowns[i](ctx))
			}
		}
		return errors.Join(errs...)
	}
}

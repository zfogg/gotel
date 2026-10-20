package gotel

import (
	"context"
	"errors"
	"sync"
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

// ShutdownStack collects provider shutdown functions while setup is in
// progress. Its method value can safely be retained before Add calls; Shutdown
// reads the functions at invocation time and runs them in reverse order.
type ShutdownStack struct {
	mu        sync.Mutex
	shutdowns []func(context.Context) error
}

// Add appends one non-nil shutdown function.
func (s *ShutdownStack) Add(shutdown func(context.Context) error) {
	if shutdown == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.shutdowns = append(s.shutdowns, shutdown)
}

// Shutdown runs the collected functions in reverse order and joins errors.
func (s *ShutdownStack) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	shutdowns := append([]func(context.Context) error(nil), s.shutdowns...)
	s.mu.Unlock()
	return Shutdown(shutdowns...)(ctx)
}

package gotel

import (
	"context"
	"reflect"
	"testing"
)

func TestShutdownStackMethodValueSeesLaterAdds(t *testing.T) {
	var stack ShutdownStack
	shutdown := stack.Shutdown
	var order []int
	stack.Add(func(context.Context) error { order = append(order, 1); return nil })
	stack.Add(func(context.Context) error { order = append(order, 2); return nil })
	if err := shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(order, []int{2, 1}) {
		t.Fatalf("shutdown order = %v", order)
	}
}

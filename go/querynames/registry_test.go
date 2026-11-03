package querynames

import (
	"strings"
	"sync"
	"testing"

	sequel "github.com/zfogg/sequel/go"
)

func TestRuntimeIdentityRegistry(t *testing.T) {
	r := NewRegistry(2)
	if err := r.Observe("Read", "SELECT 1", sequel.DialectPostgreSQL); err != nil {
		t.Fatal(err)
	}
	if err := r.Observe("Read", "SELECT 2", sequel.DialectPostgreSQL); err != nil {
		t.Fatal(err)
	}
	if err := r.Observe("Read", "DELETE FROM widgets", sequel.DialectPostgreSQL); err == nil {
		t.Fatal("name reused for another definition")
	}
	if err := r.Observe("Other", "SELECT 3", sequel.DialectPostgreSQL); err != nil {
		t.Fatalf("distinct source names may share a runtime shape: %v", err)
	}
	if err := r.Observe("Update", "UPDATE widgets SET id=1", sequel.DialectPostgreSQL); err == nil {
		t.Fatal("registry limit ignored")
	}
	if err := r.Observe("Bad", "SELECT 'private-value", sequel.DialectPostgreSQL); err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("unsafe malformed identity")
	}
	if err := r.ObserveFingerprint("Name", "private-query-text"); err == nil {
		t.Fatal("unversioned identity accepted")
	}
}

func TestRuntimeDefinitionRegistryPreservesFixedSemantics(t *testing.T) {
	r := NewRegistry(2)
	if err := r.ObserveDefinition("Fill", "-- name: Fill :one\nSELECT value FROM alerts WHERE key='fill'", sequel.DialectPostgreSQL); err != nil {
		t.Fatal(err)
	}
	if err := r.ObserveDefinition("Alert", "-- name: Alert :one\nSELECT value FROM alerts WHERE key=$1", sequel.DialectPostgreSQL); err != nil {
		t.Fatal(err)
	}
	if err := r.ObserveDefinition("Fill", "SELECT value FROM alerts WHERE key='other'", sequel.DialectPostgreSQL); err == nil {
		t.Fatal("same source name changed fixed literal")
	}
	if err := NewRegistry(2).ObserveDefinition("Bad", "SELECT 'private-value", sequel.DialectPostgreSQL); err == nil || strings.Contains(err.Error(), "private-value") {
		t.Fatal("unsafe malformed source error")
	}
	r = NewRegistry(2)
	if err := r.ObserveDefinition("One", "-- name: One :one\nSELECT 'same'", sequel.DialectPostgreSQL); err != nil {
		t.Fatal(err)
	}
	if err := r.ObserveDefinition("Two", "-- name: Two :one\nSELECT 'same'", sequel.DialectPostgreSQL); err == nil {
		t.Fatal("duplicate exact source body accepted")
	}
	r = NewRegistryWithConfig(RegistryConfig{AllowDuplicateDefinitions: true})
	if err := r.ObserveDefinition("One", "SELECT 'same'", sequel.DialectPostgreSQL); err != nil {
		t.Fatal(err)
	}
	if err := r.ObserveDefinition("Two", "SELECT 'same'", sequel.DialectPostgreSQL); err != nil {
		t.Fatal(err)
	}
	if err := r.ObserveDefinition("One", "SELECT 'different'", sequel.DialectPostgreSQL); err == nil {
		t.Fatal("duplicate policy disabled same-name consistency")
	}
}

func TestRuntimeRegistryConcurrent(t *testing.T) {
	r := NewRegistry(1)
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 20 {
				if err := r.Observe("Read", "SELECT 1", sequel.DialectGeneric); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
}

func TestRuntimeDefinitionRegistryConcurrent(t *testing.T) {
	r := NewRegistry(2)
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			name, sql := "One", "SELECT 'one'"
			if i%2 != 0 {
				name, sql = "Two", "SELECT 'two'"
			}
			for range 20 {
				if err := r.ObserveDefinition(name, sql, sequel.DialectGeneric); err != nil {
					t.Error(err)
				}
			}
		})
	}
	wg.Wait()
}

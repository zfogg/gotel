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
	if err := r.Observe("Other", "SELECT 3", sequel.DialectPostgreSQL); err == nil {
		t.Fatal("duplicate definition accepted")
	}
	if err := r.Observe("Delete", "DELETE FROM widgets", sequel.DialectPostgreSQL); err != nil {
		t.Fatal(err)
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

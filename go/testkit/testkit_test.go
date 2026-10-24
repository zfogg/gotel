package testkit

import "testing"

func TestValidateSpanName(t *testing.T) {
	if err := ValidateSpanName("ratio.http.server.GET /orders/{id}"); err != nil {
		t.Fatal(err)
	}
	if err := ValidateSpanName("ratio.http.server.GET /orders?user=1"); err == nil {
		t.Fatal("expected query rejection")
	}
	if err := ValidateSpanName("work.143668dc-4863-4989-875f-2ed6fa3870f2"); err == nil {
		t.Fatal("expected UUID rejection")
	}
}

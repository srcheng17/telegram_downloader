package httpv2

import (
	"strings"
	"testing"
)

func TestBuildTaskListFilter(t *testing.T) {
	filterSQL, args := buildTaskListFilter("RUNNING", " demo ")

	if !strings.Contains(filterSQL, "status = $1") {
		t.Fatalf("expected filter to include status placeholder, got %q", filterSQL)
	}
	if !strings.Contains(filterSQL, "ILIKE") {
		t.Fatalf("expected filter to include keyword search, got %q", filterSQL)
	}
	if len(args) != 2 {
		t.Fatalf("expected 2 args, got %d (%#v)", len(args), args)
	}
	if args[0] != "RUNNING" {
		t.Fatalf("expected first arg RUNNING, got %#v", args[0])
	}
	if args[1] != "demo" {
		t.Fatalf("expected second arg demo, got %#v", args[1])
	}
}

package services

import (
	"testing"
	"time"
)

func TestNormalizeServiceExecTimeout(t *testing.T) {
	tests := []struct {
		name    string
		input   time.Duration
		expect  time.Duration
	}{
		{name: "default when zero", input: 0, expect: defaultServiceExecTimeout},
		{name: "default when negative", input: -5 * time.Second, expect: defaultServiceExecTimeout},
		{name: "keep valid", input: 45 * time.Second, expect: 45 * time.Second},
		{name: "clamp max", input: 10 * time.Minute, expect: maxServiceExecTimeout},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeServiceExecTimeout(tc.input)
			if got != tc.expect {
				t.Fatalf("timeout=%v want %v", got, tc.expect)
			}
		})
	}
}

func TestNormalizeServiceExecOutputSize(t *testing.T) {
	tests := []struct {
		name   string
		input  int
		expect int
	}{
		{name: "default when zero", input: 0, expect: defaultServiceExecOutputSize},
		{name: "default when negative", input: -1, expect: defaultServiceExecOutputSize},
		{name: "minimum floor", input: 64, expect: 1024},
		{name: "keep valid", input: 32 * 1024, expect: 32 * 1024},
		{name: "clamp max", input: 1024 * 1024, expect: maxServiceExecOutputSize},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeServiceExecOutputSize(tc.input)
			if got != tc.expect {
				t.Fatalf("size=%d want %d", got, tc.expect)
			}
		})
	}
}

func TestLimitedOutputBuffer(t *testing.T) {
	b := newLimitedOutputBuffer(5)
	n, err := b.Write([]byte("hello"))
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if n != 5 {
		t.Fatalf("write count=%d want 5", n)
	}
	if b.String() != "hello" {
		t.Fatalf("buffer=%q want hello", b.String())
	}
	if b.Truncated() {
		t.Fatalf("expected truncated=false")
	}

	n, err = b.Write([]byte(" world"))
	if err != nil {
		t.Fatalf("unexpected write error: %v", err)
	}
	if n != 6 {
		t.Fatalf("write count=%d want 6", n)
	}
	if b.String() != "hello" {
		t.Fatalf("buffer=%q want hello", b.String())
	}
	if !b.Truncated() {
		t.Fatalf("expected truncated=true")
	}
}

package main

import "testing"

func TestIsLoopback(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1:8765": true,
		"[::1]:8765":     true,
		"localhost:8765": true,
		":8765":          false,
		"0.0.0.0:8765":   false,
		"10.0.0.5:8765":  false,
	}
	for addr, want := range cases {
		got, err := isLoopback(addr)
		if err != nil {
			t.Fatalf("%s: %v", addr, err)
		}
		if got != want {
			t.Errorf("isLoopback(%q) = %v, want %v", addr, got, want)
		}
	}
	if _, err := isLoopback("nonsense"); err == nil {
		t.Error("expected error for address without port")
	}
}

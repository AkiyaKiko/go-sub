package controller

import (
	"net/http/httptest"
	"os"
	"testing"
)

func TestSameOriginProtectionRejectsCrossOriginWrite(t *testing.T) {
	server := NewServer(os.DirFS(".."))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "http://127.0.0.1/api/login", nil)
	request.Header.Set("Origin", "http://evil.example")

	server.router.ServeHTTP(recorder, request)

	if recorder.Code != statusForbidden {
		t.Fatalf("cross-origin POST status = %d, want %d", recorder.Code, statusForbidden)
	}
}

func TestSameOriginProtectionAllowsSameOriginWrite(t *testing.T) {
	server := NewServer(os.DirFS(".."))
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest("POST", "http://127.0.0.1/api/login", nil)
	request.Header.Set("Origin", "http://127.0.0.1")

	server.router.ServeHTTP(recorder, request)

	if recorder.Code == statusForbidden {
		t.Fatalf("same-origin POST was forbidden")
	}
}

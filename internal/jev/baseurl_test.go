package jev_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/zoidsh/tenetlint/internal/jev"
)

func TestBaseURLFromEnv(t *testing.T) {
	var asked bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		asked = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{"q":{"type":"noul","noul":0.5}},"usage":{"input_tokens":1}}`))
	}))
	defer server.Close()
	t.Setenv(jev.BaseURLEnv, server.URL)

	questions := map[string]jev.Question{"q": jev.Noul("Is it so?", "", "")}
	if _, err := jev.New("key").Ask(context.Background(), "state", questions); err != nil {
		t.Fatal(err)
	}
	if !asked {
		t.Errorf("%s did not redirect the client", jev.BaseURLEnv)
	}
}

func TestBaseURLOptionOutranksEnv(t *testing.T) {
	var wanted bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		wanted = true
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"model":"jev-1.13.0","answers":{},"usage":{"input_tokens":1}}`))
	}))
	defer server.Close()
	t.Setenv(jev.BaseURLEnv, "http://127.0.0.1:1")

	questions := map[string]jev.Question{"q": jev.Noul("Is it so?", "", "")}
	client := jev.New("key", jev.WithBaseURL(server.URL))
	if _, err := client.Ask(context.Background(), "state", questions); err != nil {
		t.Fatal(err)
	}
	if !wanted {
		t.Error("WithBaseURL did not outrank the environment")
	}
}

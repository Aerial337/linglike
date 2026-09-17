package translate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestLLMEndpoint(t *testing.T) {
	cases := map[string]string{
		"":                          "http://localhost:11434/v1/chat/completions",
		"http://localhost:11434":    "http://localhost:11434/v1/chat/completions",
		"http://localhost:1234/v1/": "http://localhost:1234/v1/chat/completions",
		"https://api.example.com/v1/chat/completions": "https://api.example.com/v1/chat/completions",
	}
	for in, want := range cases {
		if got := (&LLM{URL: in}).Endpoint(); got != want {
			t.Errorf("Endpoint(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLLMTranslate(t *testing.T) {
	var gotReq chatRequest
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			http.NotFound(w, r)
			return
		}
		gotAuth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&gotReq)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"<think>hmm</think>\n\"Guten Morgen\"\n"}}]}`))
	}))
	defer srv.Close()

	l := &LLM{URL: srv.URL, APIKey: "secret", Model: "test-model", Prompt: "Translate to {target} from {source}: {text}"}
	res, err := l.Translate(context.Background(), "good morning", "auto", "de")
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Guten Morgen" {
		t.Errorf("text = %q", res.Text)
	}
	if gotAuth != "Bearer secret" || gotReq.Model != "test-model" || gotReq.Stream {
		t.Errorf("request = %+v auth=%q", gotReq, gotAuth)
	}
	if len(gotReq.Messages) != 1 || gotReq.Messages[0].Content != "Translate to German from the detected language: good morning" {
		t.Errorf("prompt = %+v", gotReq.Messages)
	}
}

func TestLLMErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		w.Write([]byte(`{"error":{"message":"model 'nope' not found"}}`))
	}))
	defer srv.Close()
	_, err := (&LLM{URL: srv.URL, Model: "nope"}).Translate(context.Background(), "hi", "auto", "fr")
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("err = %v", err)
	}
	_, err = (&LLM{URL: srv.URL}).Translate(context.Background(), "hi", "auto", "fr")
	if err == nil || !strings.Contains(err.Error(), "model") {
		t.Fatalf("missing model err = %v", err)
	}
	if got := (&LLM{}).BuildPrompt("x", "auto", "French"); !strings.Contains(got, "into French") || !strings.HasSuffix(got, "\n\nx") {
		t.Errorf("default prompt: %q", got)
	}
}

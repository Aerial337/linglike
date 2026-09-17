package translate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// LLM translates through an OpenAI-compatible chat completions endpoint,
// as served by Ollama, LM Studio, llama.cpp, vLLM, LocalAI, and hosted
// providers.
type LLM struct {
	// URL of the server. Either the full chat completions endpoint
	// (http://localhost:11434/v1/chat/completions) or just the base
	// (http://localhost:11434 or http://localhost:1234/v1).
	URL    string
	APIKey string
	Model  string
	// Prompt is the instruction sent to the model. Placeholders:
	// {text} the text to translate, {target} the target language name,
	// {source} the source language name (or "the detected language").
	Prompt      string
	Temperature float64
	Timeout     time.Duration
	HTTP        *http.Client
}

// DefaultLLMPrompt is used when no prompt is configured.
const DefaultLLMPrompt = "You are a professional translator. Translate the following text from {source} into {target}. " +
	"Reply with the translation only, without explanations, notes or quotation marks. Keep the original formatting and line breaks.\n\n{text}"

// Endpoint returns the chat completions URL derived from URL.
func (l *LLM) Endpoint() string {
	u := strings.TrimSpace(l.URL)
	if u == "" {
		u = "http://localhost:11434"
	}
	u = strings.TrimRight(u, "/")
	lower := strings.ToLower(u)
	switch {
	case strings.HasSuffix(lower, "/chat/completions"):
		return u
	case strings.HasSuffix(lower, "/v1"):
		return u + "/chat/completions"
	default:
		return u + "/v1/chat/completions"
	}
}

// BuildPrompt fills the placeholders of the prompt template.
func (l *LLM) BuildPrompt(text, source, target string) string {
	tmpl := l.Prompt
	if strings.TrimSpace(tmpl) == "" {
		tmpl = DefaultLLMPrompt
	}
	src := "the detected language"
	if source != "" && source != "auto" {
		src = LanguageName(source)
	}
	r := strings.NewReplacer("{text}", text, "{target}", target, "{source}", src)
	out := r.Replace(tmpl)
	if !strings.Contains(tmpl, "{text}") {
		out += "\n\n" + text
	}
	return out
}

type chatRequest struct {
	Model       string        `json:"model"`
	Messages    []chatMessage `json:"messages"`
	Temperature float64       `json:"temperature"`
	Stream      bool          `json:"stream"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		Text string `json:"text"`
	} `json:"choices"`
	Error *struct {
		Message string `json:"message"`
	} `json:"error"`
	// Ollama's native error format
	ErrorText string `json:"-"`
}

var thinkRe = regexp.MustCompile(`(?s)<think>.*?</think>\s*`)

// Translate sends the text to the model. target is a language name (or
// code, which is mapped to its name).
func (l *LLM) Translate(ctx context.Context, text, source, target string) (*Result, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, errors.New("nothing to translate")
	}
	if strings.TrimSpace(l.Model) == "" {
		return nil, errors.New("no model name configured for the local LLM")
	}
	targetName := target
	if n, ok := languageNames[target]; ok {
		targetName = n
	}
	body, err := json.Marshal(chatRequest{
		Model:       l.Model,
		Messages:    []chatMessage{{Role: "user", Content: l.BuildPrompt(text, source, targetName)}},
		Temperature: l.Temperature,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, "POST", l.Endpoint(), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if k := strings.TrimSpace(l.APIKey); k != "" {
		req.Header.Set("Authorization", "Bearer "+k)
	}
	client := l.HTTP
	if client == nil {
		timeout := l.Timeout
		if timeout <= 0 {
			timeout = 90 * time.Second
		}
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", l.Endpoint(), err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	var cr chatResponse
	if jerr := json.Unmarshal(raw, &cr); jerr != nil {
		if resp.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(raw))
		}
		return nil, fmt.Errorf("unexpected response: %s", snippet(raw))
	}
	if cr.Error != nil && cr.Error.Message != "" {
		return nil, errors.New(cr.Error.Message)
	}
	if resp.StatusCode != 200 {
		var generic map[string]any
		if json.Unmarshal(raw, &generic) == nil {
			if e, ok := generic["error"].(string); ok && e != "" {
				return nil, errors.New(e)
			}
		}
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, snippet(raw))
	}
	if len(cr.Choices) == 0 {
		return nil, errors.New("empty response from the model")
	}
	out := cr.Choices[0].Message.Content
	if out == "" {
		out = cr.Choices[0].Text
	}
	out = thinkRe.ReplaceAllString(out, "")
	out = strings.TrimSpace(out)
	// Models sometimes wrap the answer in quotes or a code fence.
	out = strings.TrimPrefix(out, "```")
	out = strings.TrimSuffix(out, "```")
	if len(out) >= 2 && out[0] == '"' && out[len(out)-1] == '"' && !strings.Contains(out[1:len(out)-1], "\"") {
		out = out[1 : len(out)-1]
	}
	out = strings.TrimSpace(out)
	if out == "" {
		return nil, errors.New("empty response from the model")
	}
	return &Result{Text: out, SourceLang: source}, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

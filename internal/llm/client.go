package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

// Client is a minimal OpenAI-compatible chat client used by kubenow.
type Client struct {
	Endpoint string        // e.g. https://api.openai.com/v1 or http://localhost:11434/v1
	Model    string        // e.g. gpt-4.1-mini, mixtral:8x22b
	APIKey   string        // optional for local; for OpenAI use --api-key or OPENAI_API_KEY
	Timeout  time.Duration // per request timeout
}

type chatRequest struct {
	Model    string        `json:"model"`
	Messages []chatMessage `json:"messages"`
	Stream   bool          `json:"stream,omitempty"`
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
	} `json:"choices"`

	Error *struct {
		Message string      `json:"message"`
		Type    string      `json:"type"`
		Code    interface{} `json:"code"`
	} `json:"error,omitempty"`
}

// Complete sends a single chat completion request and returns the content of the first choice.
func (c Client) Complete(ctx context.Context, prompt string) (string, error) {
	if c.Timeout <= 0 {
		c.Timeout = 60 * time.Second
	}

	// Resolve API key:
	// 1) explicit --api-key wins
	// 2) else OPENAI_API_KEY from env (for any OpenAI-compatible cloud endpoint)
	if c.APIKey == "" {
		if env := os.Getenv("OPENAI_API_KEY"); env != "" {
			c.APIKey = env
		}
	}

	reqBody := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "user", Content: prompt},
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	url := strings.TrimRight(c.Endpoint, "/") + "/chat/completions"

	httpClient := &http.Client{Timeout: c.Timeout}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Only set Authorization when we actually have a key.
	if c.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("http do: %w", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		// Let caller see both status code and body for debugging (401, 429, etc.)
		return "", fmt.Errorf("%d %s: %s", resp.StatusCode, http.StatusText(resp.StatusCode), string(body))
	}

	var cr chatResponse
	if err := json.Unmarshal(body, &cr); err != nil {
		return "", fmt.Errorf("decode response: %w (raw: %s)", err, string(body))
	}

	if cr.Error != nil {
		return "", fmt.Errorf("llm error: %s", cr.Error.Message)
	}

	if len(cr.Choices) == 0 {
		return "", fmt.Errorf("no choices in response")
	}

	return cr.Choices[0].Message.Content, nil
}

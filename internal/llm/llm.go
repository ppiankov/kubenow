//  Minimal OpenAI-compatible client.

package llm

import (
    "bytes"
    "context"
    "encoding/json"
    "fmt"
    "io"
    "net/http"
)

type ChatRequest struct {
    Model    string       `json:"model"`
    Messages []ChatMessage `json:"messages"`
}

type ChatMessage struct {
    Role    string `json:"role"`
    Content string `json:"content"`
}

type ChatResponse struct {
    Choices []struct {
        Message ChatMessage `json:"message"`
    } `json:"choices"`
}

func QueryLLM(ctx context.Context, endpoint, apiKey, model, prompt string) (string, error) {
    reqBody := ChatRequest{
        Model: model,
        Messages: []ChatMessage{
            {Role: "user", Content: prompt},
        },
    }

    buf, _ := json.Marshal(reqBody)
    req, err := http.NewRequestWithContext(ctx, "POST", endpoint+"/chat/completions", bytes.NewBuffer(buf))
    if err != nil {
        return "", err
    }

    req.Header.Set("Content-Type", "application/json")
    if apiKey != "" {
        req.Header.Set("Authorization", "Bearer "+apiKey)
    }

    resp, err := http.DefaultClient.Do(req)
    if err != nil {
        return "", err
    }
    defer resp.Body.Close()

    if resp.StatusCode != 200 {
        b, _ := io.ReadAll(resp.Body)
        return "", fmt.Errorf("llm error: %s: %s", resp.Status, string(b))
    }

    var out ChatResponse
    if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
        return "", err
    }

    if len(out.Choices) == 0 {
        return "", fmt.Errorf("empty response")
    }

    return out.Choices[0].Message.Content, nil
}

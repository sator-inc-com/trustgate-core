package adapter

import (
	"context"
	"fmt"
	"time"

	"github.com/trustgate/trustgate/internal/config"
)

// MockAdapter echoes user input as the assistant response.
// Used with `aigw serve --mock-backend` for development without AWS credentials.
type MockAdapter struct {
	delay time.Duration
}

// NewMockAdapter creates a mock adapter with the given configuration.
func NewMockAdapter(cfg config.MockConfig) *MockAdapter {
	return &MockAdapter{
		delay: time.Duration(cfg.DelayMs) * time.Millisecond,
	}
}

func (m *MockAdapter) Invoke(ctx context.Context, req LLMRequest) (LLMResponse, error) {
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-ctx.Done():
			return LLMResponse{}, ctx.Err()
		}
	}

	// Extract last user message for echo response
	lastUserContent := ""
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == "user" {
			lastUserContent = req.Messages[i].Content
			break
		}
	}

	responseContent := fmt.Sprintf("[Mock Response] Received: %s", lastUserContent)

	// Approximate token counts
	promptTokens := len(ContentString(req.Messages)) / 4
	completionTokens := len(responseContent) / 4
	if promptTokens == 0 {
		promptTokens = 1
	}
	if completionTokens == 0 {
		completionTokens = 1
	}

	model := req.Model
	if model == "" {
		model = "mock-model"
	}

	return LLMResponse{
		ID:      fmt.Sprintf("mock-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []Choice{
			{
				Index: 0,
				Message: Message{
					Role:    "assistant",
					Content: responseContent,
				},
				FinishReason: "stop",
			},
		},
		Usage: Usage{
			PromptTokens:     promptTokens,
			CompletionTokens: completionTokens,
			TotalTokens:      promptTokens + completionTokens,
		},
	}, nil
}

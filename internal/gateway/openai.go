package gateway

import (
	"fmt"
	"time"

	"github.com/trustgate/trustgate/internal/adapter"
)

// newBlockedResponse creates an OpenAI-compatible response for blocked requests.
func newBlockedResponse(model, auditID, message string) adapter.LLMResponse {
	if message == "" {
		message = "This request was blocked by security policy."
	}
	return adapter.LLMResponse{
		ID:      fmt.Sprintf("tg-%s", auditID),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []adapter.Choice{
			{
				Index: 0,
				Message: adapter.Message{
					Role:    "assistant",
					Content: message,
				},
				FinishReason: "blocked",
			},
		},
		Usage: adapter.Usage{},
	}
}

// newContentFilterResponse creates an OpenAI-compatible response when output is filtered.
func newContentFilterResponse(model, auditID string) adapter.LLMResponse {
	return adapter.LLMResponse{
		ID:      fmt.Sprintf("tg-%s", auditID),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   model,
		Choices: []adapter.Choice{
			{
				Index: 0,
				Message: adapter.Message{
					Role:    "assistant",
					Content: "The response was filtered by content security policy.",
				},
				FinishReason: "content_filter",
			},
		},
		Usage: adapter.Usage{},
	}
}

// openAIError represents an OpenAI-compatible error response.
type openAIError struct {
	Error openAIErrorDetail `json:"error"`
}

type openAIErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

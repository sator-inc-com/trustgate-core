package adapter

import "context"

// Adapter sends requests to an LLM backend and returns responses.
type Adapter interface {
	// Invoke sends a non-streaming request to the LLM backend.
	Invoke(ctx context.Context, req LLMRequest) (LLMResponse, error)
}

// LLMRequest represents an OpenAI-compatible chat completion request.
type LLMRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature *float64  `json:"temperature,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	Stop        []string  `json:"stop,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// Message represents a chat message (user, assistant, system, or tool).
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall represents a function call made by the LLM.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // "function"
	Function FunctionCall `json:"function"`
}

// FunctionCall contains the function name and arguments.
type FunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// LLMResponse represents an OpenAI-compatible chat completion response.
type LLMResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

// Choice represents a single completion choice.
type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

// Usage reports token usage for the request.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ContentString extracts the text content from all messages for detection.
// It concatenates user, assistant, and system message contents with newlines.
func ContentString(messages []Message) string {
	var result string
	for _, m := range messages {
		if m.Content != "" {
			if result != "" {
				result += "\n"
			}
			result += m.Content
		}
		// Also include tool call arguments for inspection
		for _, tc := range m.ToolCalls {
			if tc.Function.Arguments != "" {
				if result != "" {
					result += "\n"
				}
				result += tc.Function.Arguments
			}
		}
	}
	return result
}

// Package model is a client of the chat-completions protocol of OpenAI, over net/http. Any server that speaks it
// works: a local model or a hosted one. It sends one request and reads one answer. It keeps no state.
package model

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
)

// maxAnswer is the largest answer body that Chat reads. A larger body is an error, never a cut body.
const maxAnswer = 8 << 20

// stepTimeout is the longest wait for one answer of the model.
const stepTimeout = 2 * time.Minute

// Message is one message of the conversation. Role is system, user, assistant or tool. An assistant message that
// carries ToolCalls goes back to the model as it came, followed by one tool message for each call.
type Message struct {
	Role       string     `json:"role"`
	Content    *string    `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

// ToolCall is a call of a function that the model asks for. Arguments is a JSON text.
type ToolCall struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"`
	Function FunctionCall `json:"function"`
}

// FunctionCall is the name of a function and its arguments as the JSON text that the model wrote.
type FunctionCall struct {
	Name      string    `json:"name"`
	Arguments Arguments `json:"arguments"`
}

// Arguments is the JSON text of the arguments of a call. The protocol sends it as a JSON string; some local servers
// send the object itself. Both are read; it is sent back as a string, as the protocol says.
type Arguments string

func (a *Arguments) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		*a = Arguments(s)
		return nil
	}
	*a = Arguments(data)
	return nil
}

// Tool is a function that the model may call. Parameters is a JSON Schema.
type Tool struct {
	Name        string
	Description string
	Parameters  map[string]any
}

// Text is a message with a role and a text.
func Text(role, text string) Message { return Message{Role: role, Content: &text} }

// Reply is the message that the model answered, and why it stopped.
type Reply struct {
	Message      Message
	FinishReason string
}

// Text is the text of the reply, "" when the model sent none.
func (r Reply) Text() string {
	if r.Message.Content == nil {
		return ""
	}
	return *r.Message.Content
}

// Client calls one model at one address.
type Client struct {
	hc    *http.Client
	base  string
	model string
	key   string
}

// New is a client of the server at base (it ends in /v1) for the model id. A key, when it is not empty, goes as a
// bearer credential. The key is never part of an error.
func New(base, model, key string) *Client {
	return &Client{hc: &http.Client{}, base: strings.TrimRight(base, "/"), model: model, key: key}
}

type request struct {
	Model    string        `json:"model"`
	Messages []Message     `json:"messages"`
	Tools    []requestTool `json:"tools,omitempty"`
}

type requestTool struct {
	Type     string      `json:"type"`
	Function requestFunc `json:"function"`
}

type requestFunc struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type answer struct {
	Choices []struct {
		Message      Message `json:"message"`
		FinishReason string  `json:"finish_reason"`
	} `json:"choices"`
}

// Chat sends the messages and the tools and returns the first choice of the answer. The call ends after two
// minutes, or when ctx ends. A status other than 2xx is an error with the status and the first line of the body.
func (c *Client) Chat(ctx context.Context, messages []Message, tools []Tool) (Reply, error) {
	ctx, cancel := context.WithTimeout(ctx, stepTimeout)
	defer cancel()
	body := request{Model: c.model, Messages: messages}
	for _, t := range tools {
		body.Tools = append(body.Tools, requestTool{Type: "function", Function: requestFunc(t)})
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return Reply{}, fmt.Errorf("model request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/chat/completions", bytes.NewReader(raw))
	if err != nil {
		return Reply{}, fmt.Errorf("model request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		if cause := errors.Unwrap(err); cause != nil { // the URL error repeats the address: keep its cause
			err = cause
		}
		return Reply{}, fmt.Errorf("model request: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxAnswer+1))
	if err != nil {
		return Reply{}, fmt.Errorf("model answer: %w", err)
	}
	if len(data) > maxAnswer {
		return Reply{}, fmt.Errorf("model answer is larger than %d bytes", maxAnswer)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		return Reply{}, fmt.Errorf("model status %d: %s", res.StatusCode, firstLine(string(data)))
	}
	var a answer
	if err := json.Unmarshal(data, &a); err != nil {
		return Reply{}, fmt.Errorf("model answer is not JSON: %w", err)
	}
	if len(a.Choices) == 0 {
		return Reply{}, errors.New("model answer has no choice")
	}
	c0 := a.Choices[0]
	c0.Message.Role = "assistant"
	return Reply{Message: c0.Message, FinishReason: c0.FinishReason}, nil
}

// firstLine is the first line of s, cut at 200 bytes on a character boundary.
func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 200 {
		n := 200
		for n > 0 && !utf8.RuneStart(s[n]) {
			n--
		}
		s = s[:n]
	}
	return s
}

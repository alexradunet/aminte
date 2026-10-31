package model

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func serve(t *testing.T, h http.HandlerFunc) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return srv
}

func TestTheRequestHasTheModelTheMessagesTheToolsAndTheKey(t *testing.T) {
	var got map[string]any
	var auth, path string
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		auth, path = r.Header.Get("Authorization"), r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("request body: %v", err)
		}
		w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	})
	c := New(srv.URL+"/v1/", "m1", "secret")
	tool := Tool{Name: "capture", Description: "write", Parameters: map[string]any{"type": "object"}}
	reply, err := c.Chat(t.Context(), []Message{Text("system", "s"), Text("user", "u")}, []Tool{tool})
	if err != nil {
		t.Fatal(err)
	}
	if reply.Text() != "ok" || reply.FinishReason != "stop" {
		t.Errorf("reply %+v", reply)
	}
	if path != "/v1/chat/completions" || auth != "Bearer secret" {
		t.Errorf("path %q, authorization %q", path, auth)
	}
	if got["model"] != "m1" {
		t.Errorf("model %v", got["model"])
	}
	msgs, _ := got["messages"].([]any)
	if len(msgs) != 2 || msgs[1].(map[string]any)["role"] != "user" || msgs[1].(map[string]any)["content"] != "u" {
		t.Errorf("messages %v", msgs)
	}
	tools, _ := got["tools"].([]any)
	fn, _ := tools[0].(map[string]any)["function"].(map[string]any)
	if tools[0].(map[string]any)["type"] != "function" || fn["name"] != "capture" || fn["description"] != "write" || fn["parameters"] == nil {
		t.Errorf("tools %v", tools)
	}
}

func TestNoKeyMeansNoAuthorizationHeader(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Header["Authorization"]; ok {
			t.Errorf("an Authorization header was sent")
		}
		w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))
	})
	if _, err := New(srv.URL, "m", "").Chat(t.Context(), nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestAReplyWithToolCalls(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":null,"tool_calls":[{"id":"c1","type":"function","function":{"name":"capture","arguments":"{\"text\":\"x\"}"}}]},"finish_reason":"tool_calls"}]}`))
	})
	reply, err := New(srv.URL, "m", "").Chat(t.Context(), nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	calls := reply.Message.ToolCalls
	if len(calls) != 1 || calls[0].ID != "c1" || calls[0].Function.Name != "capture" || calls[0].Function.Arguments != `{"text":"x"}` {
		t.Errorf("calls %+v", calls)
	}
	if reply.Text() != "" || reply.FinishReason != "tool_calls" {
		t.Errorf("reply %+v", reply)
	}
}

func TestAnAssistantMessageWithCallsGoesBackAsItCame(t *testing.T) {
	var got struct{ Messages []map[string]any }
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&got)
		w.Write([]byte(`{"choices":[{"message":{"content":"done"}}]}`))
	})
	call := ToolCall{ID: "c1", Type: "function", Function: FunctionCall{Name: "find", Arguments: "{}"}}
	result := "r"
	_, err := New(srv.URL, "m", "").Chat(t.Context(), []Message{
		{Role: "assistant", ToolCalls: []ToolCall{call}},
		{Role: "tool", ToolCallID: "c1", Content: &result},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Messages[0]["tool_calls"].([]any)) != 1 || got.Messages[1]["tool_call_id"] != "c1" || got.Messages[1]["content"] != "r" {
		t.Errorf("messages %v", got.Messages)
	}
}

func TestANon2xxStatusIsAnErrorWithTheFirstLine(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte("slow down\nsecond line"))
	})
	_, err := New(srv.URL, "m", "secret").Chat(t.Context(), nil, nil)
	if err == nil || !strings.Contains(err.Error(), "429") || !strings.Contains(err.Error(), "slow down") || strings.Contains(err.Error(), "second") {
		t.Fatalf("error %v", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Errorf("the key is in the error: %v", err)
	}
}

func TestMalformedAnswersAreErrors(t *testing.T) {
	for name, body := range map[string]string{"not JSON": "<html>", "no choice": `{"choices":[]}`} {
		t.Run(name, func(t *testing.T) {
			srv := serve(t, func(w http.ResponseWriter, r *http.Request) { w.Write([]byte(body)) })
			if _, err := New(srv.URL, "m", "").Chat(t.Context(), nil, nil); err == nil {
				t.Error("no error")
			}
		})
	}
}

func TestABodyOverTheLimitIsAnError(t *testing.T) {
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"choices":[{"message":{"content":"` + strings.Repeat("a", maxAnswer) + `"}}]}`))
	})
	reply, err := New(srv.URL, "m", "").Chat(t.Context(), nil, nil)
	if err == nil || reply.Text() != "" {
		t.Fatalf("reply %v, error %v, want an error and no text", reply.Text(), err)
	}
}

func TestACancelledContextEndsTheCall(t *testing.T) {
	release := make(chan struct{})
	srv := serve(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := New(srv.URL, "m", "").Chat(ctx, nil, nil)
	close(release)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error %v, want context.Canceled", err)
	}
}

// TestArgumentsComeAsAStringOrAsTheObject: the protocol sends the arguments as a JSON string; some local servers
// send the object itself. Both are read, and the call goes back to the model as a string.
func TestArgumentsComeAsAStringOrAsTheObject(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{`"{\"day\":\"today\"}"`, `{"day":"today"}`},
		{`{"day":"today"}`, `{"day":"today"}`},
	} {
		var a Arguments
		if err := json.Unmarshal([]byte(tc.in), &a); err != nil || string(a) != tc.want {
			t.Errorf("Unmarshal(%s) = %q, %v; want %q", tc.in, a, err, tc.want)
		}
	}
	out, err := json.Marshal(FunctionCall{Name: "x", Arguments: `{"a":1}`})
	if err != nil || string(out) != `{"name":"x","arguments":"{\"a\":1}"}` {
		t.Errorf("Marshal = %s, %v", out, err)
	}
}

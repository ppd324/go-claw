package llm

import "testing"

func TestMessagesWithSystemPrompt(t *testing.T) {
	req := &ChatRequest{
		SystemPrompt: "remember the user's name",
		Messages:     []Message{{Role: "user", Content: "what is my name?"}},
	}
	messages := messagesWithSystemPrompt(req)
	if len(messages) != 2 || messages[0].Role != "system" || messages[0].Content != req.SystemPrompt {
		t.Fatalf("system prompt was not prepended: %#v", messages)
	}
	if len(req.Messages) != 1 || req.Messages[0].Role != "user" {
		t.Fatalf("request messages were mutated: %#v", req.Messages)
	}
}

func TestMessagesWithoutSystemPrompt(t *testing.T) {
	req := &ChatRequest{Messages: []Message{{Role: "user", Content: "hello"}}}
	messages := messagesWithSystemPrompt(req)
	if len(messages) != 1 || messages[0].Role != "user" {
		t.Fatalf("unexpected messages: %#v", messages)
	}
}

package openai

import (
	"encoding/json"
	"testing"

	"github.com/go-orca/go-orca/internal/provider/common"
)

func TestBuildChatParamsUsesDefaultModelAndTools(t *testing.T) {
	params, err := buildChatParams(common.ChatRequest{
		Messages: []common.Message{
			{Role: common.RoleUser, Content: "What is the weather?"},
		},
		Tools: []common.ToolDefinition{
			{
				Name:        "get_weather",
				Description: "Fetch weather by city",
				Parameters: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"city": map[string]interface{}{"type": "string"},
					},
				},
			},
		},
		JSONMode:     true,
		OutputSchema: map[string]any{"type": "object"},
	}, "gpt-4o")
	if err != nil {
		t.Fatalf("buildChatParams returned error: %v", err)
	}

	if got := string(params.Model); got != "gpt-4o" {
		t.Fatalf("model = %q, want gpt-4o", got)
	}
	if len(params.Tools) != 1 {
		t.Fatalf("tools len = %d, want 1", len(params.Tools))
	}
	if got := params.Tools[0].Function.Name; got != "get_weather" {
		t.Fatalf("tool name = %q, want get_weather", got)
	}
	if params.ResponseFormat.OfJSONSchema != nil || params.ResponseFormat.OfJSONObject != nil {
		t.Fatal("response_format should be omitted when tools are provided")
	}
}

func TestBuildChatParamsRequiresModel(t *testing.T) {
	_, err := buildChatParams(common.ChatRequest{
		Messages: []common.Message{{Role: common.RoleUser, Content: "hello"}},
	}, "")
	if err == nil {
		t.Fatal("expected missing model error")
	}
}

func TestConvertToOpenAIMessagesIncludesToolCallsAndToolResults(t *testing.T) {
	messages, err := convertToOpenAIMessages([]common.Message{
		{
			Role:    common.RoleAssistant,
			Content: "",
			ToolCalls: []common.ToolCall{
				{
					ID:   "call_123",
					Name: "get_weather",
					Arguments: map[string]interface{}{
						"city": "Honolulu",
					},
				},
			},
		},
		{
			Role:       common.RoleTool,
			Content:    `{"forecast":"sunny"}`,
			ToolCallID: "call_123",
		},
	})
	if err != nil {
		t.Fatalf("convertToOpenAIMessages returned error: %v", err)
	}
	if len(messages) != 2 {
		t.Fatalf("messages len = %d, want 2", len(messages))
	}

	calls := messages[0].GetToolCalls()
	if len(calls) != 1 {
		t.Fatalf("tool calls len = %d, want 1", len(calls))
	}
	if calls[0].ID != "call_123" || calls[0].Function.Name != "get_weather" {
		t.Fatalf("unexpected tool call: %#v", calls[0])
	}
	var args map[string]string
	if err := json.Unmarshal([]byte(calls[0].Function.Arguments), &args); err != nil {
		t.Fatalf("tool call arguments are not JSON: %v", err)
	}
	if args["city"] != "Honolulu" {
		t.Fatalf("tool call city = %q, want Honolulu", args["city"])
	}

	toolCallID := messages[1].GetToolCallID()
	if toolCallID == nil || *toolCallID != "call_123" {
		t.Fatalf("tool_call_id = %v, want call_123", toolCallID)
	}
}

func TestConvertToOpenAIMessagesRejectsToolMessageWithoutID(t *testing.T) {
	_, err := convertToOpenAIMessages([]common.Message{
		{Role: common.RoleTool, Content: "missing id"},
	})
	if err == nil {
		t.Fatal("expected missing tool_call_id error")
	}
}

func TestDecodeToolArguments(t *testing.T) {
	args := decodeToolArguments(`{"ok":true}`)
	if got, ok := args["ok"].(bool); !ok || !got {
		t.Fatalf("decoded ok = %v, want true", args["ok"])
	}

	args = decodeToolArguments(`not-json`)
	if got := args["_raw"]; got != "not-json" {
		t.Fatalf("raw fallback = %v, want not-json", got)
	}
}

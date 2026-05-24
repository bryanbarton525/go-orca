package ollama

import (
	"errors"
	"testing"
)

func TestNativeToolCallingUnstable(t *testing.T) {
	tests := []struct {
		model string
		want  bool
	}{
		{"qwen3.5:9b", true},
		{"qwen3:1.7b", true},
		{"qwen2.5-coder:14b", false},
		{"gpt-oss:20b", false},
		{"llama3.2:3b", false},
	}
	for _, tt := range tests {
		if got := NativeToolCallingUnstable(tt.model); got != tt.want {
			t.Errorf("NativeToolCallingUnstable(%q) = %v, want %v", tt.model, got, tt.want)
		}
	}
}

func TestIsRecoverableToolCallError(t *testing.T) {
	err := errors.New(`ollama: chat error: 500 Internal Server Error: XML syntax error on line 6: element <function> closed by </parameter>`)
	if !IsRecoverableToolCallError(err) {
		t.Fatal("expected recoverable")
	}
	if IsRecoverableToolCallError(errors.New("connection refused")) {
		t.Fatal("expected non-recoverable")
	}
}

package ollama

import "strings"

// NativeToolCallingUnstable reports models where Ollama's native /api/chat tools
// path frequently returns HTTP 500 on malformed or truncated Qwen3 XML tool
// output (see ollama/ollama#14570, #14834). go-orca avoids the native tool
// loop for these models and prefers stable alternatives in routing.
func NativeToolCallingUnstable(model string) bool {
	n := strings.ToLower(strings.TrimSpace(model))
	if !strings.Contains(n, "qwen3") {
		return false
	}
	// qwen2.5-* is a different family and is generally stable with tools.
	if strings.Contains(n, "qwen2.5") {
		return false
	}
	return true
}

// IsRecoverableToolCallError reports Ollama errors where aborting the tool
// loop and continuing with partial context is safer than failing the persona.
func IsRecoverableToolCallError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "xml syntax error") ||
		strings.Contains(msg, "tool call parsing failed") ||
		strings.Contains(msg, "failed to parse json") ||
		strings.Contains(msg, "unexpected end of json input")
}

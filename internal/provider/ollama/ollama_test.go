package ollama

import (
	"net/http"
	"testing"
)

func TestBuildTransportClonesDefaultTransportAndAppliesTLSSkipVerify(t *testing.T) {
	original, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("unexpected default transport type %T", http.DefaultTransport)
	}
	originalSkip := original.TLSClientConfig != nil && original.TLSClientConfig.InsecureSkipVerify

	transport, err := buildTransport(true)
	if err != nil {
		t.Fatalf("buildTransport: %v", err)
	}
	if transport == original {
		t.Fatal("expected cloned transport, got default transport")
	}
	if transport.TLSClientConfig == nil || !transport.TLSClientConfig.InsecureSkipVerify {
		t.Fatal("expected tls skip verify to be enabled")
	}
	if original.TLSClientConfig != nil && original.TLSClientConfig.InsecureSkipVerify != originalSkip {
		t.Fatal("default transport was mutated")
	}
	if original.TLSClientConfig == nil && originalSkip {
		t.Fatal("default transport was mutated")
	}
}

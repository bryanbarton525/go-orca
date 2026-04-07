// Package persona defines the Persona interface and the execution contract
// that all built-in and custom personas must satisfy.
package persona

import (
	"context"

	"github.com/go-orca/go-orca/internal/state"
)

// Persona is the interface that all workflow personas must implement.
// Built-in personas are registered at startup; custom personas can be
// added via the component registry.
type Persona interface {
	// Kind returns the unique role identifier for this persona.
	Kind() state.PersonaKind

	// Name returns a human-readable display name.
	Name() string

	// Description describes this persona's role and responsibilities.
	Description() string

	// Execute runs the persona against the supplied HandoffPacket and returns
	// its typed output.  The context carries deadline and cancellation signals.
	Execute(ctx context.Context, packet state.HandoffPacket) (*state.PersonaOutput, error)
}

// Registry holds all registered personas, keyed by their Kind.
var registry = map[state.PersonaKind]Persona{}

// Register adds a persona to the global registry.
// Panics on duplicate registration.
func Register(p Persona) {
	if _, exists := registry[p.Kind()]; exists {
		panic("persona: duplicate registration: " + string(p.Kind()))
	}
	registry[p.Kind()] = p
}

// Get returns the named persona, or (nil, false) if it is not registered.
func Get(kind state.PersonaKind) (Persona, bool) {
	p, ok := registry[kind]
	return p, ok
}

// All returns all registered personas.
func All() []Persona {
	out := make([]Persona, 0, len(registry))
	for _, p := range registry {
		out = append(out, p)
	}
	return out
}

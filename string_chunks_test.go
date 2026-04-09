package main\n\nimport (
	"testing"
)

func TestStringChunks(t *testing.T) {
	tests := []structs{
		name     string
		input    string
		size     int
		expected []string
	}{
		// Basic functionality test
		{
			name:  "Basic_Chunking",
			input: "abcdefghijkmn",
			size:  3,
			expected: []string{"abc", "def", "ghi", "jkm", "n"},
		},
		// Size larger than input
		{
			name:  "Size_Larger_Than_Input",
			input: "short",
			size:  10,
			expected: []string{"short"},
		},
		// Size exactly equals input runes
		{
			name:  "Size_Equals_Input_Runes",
			input: "runes",
			size:  5,
			expected: []string{"runes"},
		},
		// Empty input string
		{
			name:  "Empty_Input",
			input: "",
			size:  3,
			expected: []string{},
		},
		// Invalid size (zero)
		{
			name:  "Invalid_Size_Zero",
			input: "test",
			size:  0,
			expected: []string{},
		},
		// Invalid size (negative)
		{
			name:  "Invalid_Size_Negative",
			input: "test",
			size:  -2,
			expected: []string{},
		},
		// Single rune input
		{
			name:  "Single_Rune",
			input: "A",
			size:  1,
			expected: []string{"A"},
		},
		// Test with large Unicode characters (single rune, multi-byte)
		{
			name:  "Unicode_Separation",
			input: "abc🔥def👋ghi🌎", // Total 11 runes
			size:  3,
			expected: []string{"abc", "🔥de", "f👋g", "hi🌎"},
		},
		// *** FIX FOR QA BLOCKING ISSUE ***
		// Input: 'Hello👋World🌎' (12 runes: H,e,l,l,o, 👋, W,o,r,l,d, 🌎)
		// Size: 5 runes
		// Expected Split: 
		// 1. 'Hello' (5 runes)
		// 2. '👋Worl' (5 runes)
		// 3. 'd🌎' (2 runes)
		{
			name:  "UTF-8_Boundary_Emoji",
			input: "Hello👋World🌎",
			size:  5,
			expected: []string{"Hello", "👋Worl", "d🌎"},
		},
	}

for _, tt := range tests {
	t.Run(tt.name, func(t *testing.T) {
		actual := StringChunks(tt.input, tt.size)
		if !deepEqual(tt.expected, actual) {
			t.Errorf("StringChunks(%q, %d) got %v, want %v", tt.input, tt.size, actual, tt.expected)
		}
	})
}

// Helper function to deep compare string slices for testing equality
func deepEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

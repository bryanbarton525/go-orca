package utils

// StringChunks splits a UTF-8 string into chunks, where each chunk contains at most `size` runes.
// It returns the chunks as a slice of strings.
// It handles edge cases for empty input, and non-positive chunk sizes by returning an empty slice.
func StringChunks(s string, size int) []string {
    // F3: Handle empty string input
    if s == "" {
        return []string{}
    }

    // F2: Handle zero or negative chunk size
    if size < 1 {
        return []string{}
    }

    // Convert to []rune to ensure accurate rune counting (Design Decision).
    runes := []rune(s)
    var chunks []string
    var currentChunkRunes []rune

    for i, r := range runes {
        currentChunkRunes = append(currentChunkRunes, r)

        // Check if the current chunk size has reached the limit OR if it is the last rune
        if len(currentChunkRunes) >= size || i == len(runes)-1 {
            // If the last rune was added, we must ensure we only append it if the limit wasn't already hit.
            // The check `i == len(runes)-1` ensures the final segment is always flushed.
            if len(currentChunkRunes) > 0 {
                chunks = append(chunks, string(currentChunkRunes))
                // Reset for the next chunk
                currentChunkRunes = nil
            }
        }
    }

    // This final check is slightly redundant given the loop logic, but ensures safety if something unexpected happens.
    // However, the loop structure already handles flushing on the last element.
    return chunks
}

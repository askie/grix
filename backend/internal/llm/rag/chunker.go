package rag

import (
	"strings"
	"unicode/utf8"
)

const (
	defaultChunkSize    = 400 // tokens (approximate)
	defaultChunkOverlap = 50
	charsPerToken       = 2 // rough estimate for Chinese text
)

// Chunk represents a text chunk with its index.
type Chunk struct {
	Index int
	Text  string
}

// ChunkText splits text into overlapping chunks.
func ChunkText(text string, chunkSizeTokens, overlapTokens int) []Chunk {
	if chunkSizeTokens <= 0 {
		chunkSizeTokens = defaultChunkSize
	}
	if overlapTokens <= 0 {
		overlapTokens = defaultChunkOverlap
	}

	chunkChars := chunkSizeTokens * charsPerToken
	overlapChars := overlapTokens * charsPerToken

	// Ensure overlap is less than chunk size to avoid infinite loop
	if overlapChars >= chunkChars {
		overlapChars = chunkChars / 2
	}

	if utf8.RuneCountInString(text) <= chunkChars {
		return []Chunk{{Index: 0, Text: text}}
	}

	runes := []rune(text)
	var chunks []Chunk
	idx := 0
	start := 0

	for start < len(runes) {
		end := start + chunkChars
		if end > len(runes) {
			end = len(runes)
		}

		// Try to break at paragraph or newline boundary
		chunk := string(runes[start:end])
		if end < len(runes) {
			// strings.LastIndex returns byte offset, convert to rune count
			if lastNewline := strings.LastIndex(chunk, "\n"); lastNewline > 0 {
				runeOffset := utf8.RuneCountInString(chunk[:lastNewline])
				if runeOffset > chunkChars/2 {
					end = start + runeOffset + 1
					chunk = string(runes[start:end])
				}
			}
		}

		chunks = append(chunks, Chunk{Index: idx, Text: strings.TrimSpace(chunk)})
		idx++

		// Ensure we always make progress to avoid infinite loop
		nextStart := end - overlapChars
		if nextStart <= start {
			nextStart = end // No overlap if it would cause no progress
		}
		start = nextStart

		if end >= len(runes) {
			break
		}
	}

	return chunks
}

package rag

import (
	"strings"
	"testing"
)

func TestChunkText(t *testing.T) {
	t.Run("empty text", func(t *testing.T) {
		chunks := ChunkText("", 100, 20)
		if len(chunks) != 1 {
			t.Errorf("expected 1 chunk for empty text, got %d", len(chunks))
		}
	})

	t.Run("short text", func(t *testing.T) {
		text := "这是一个短文本"
		chunks := ChunkText(text, 100, 20)

		if len(chunks) != 1 {
			t.Errorf("expected 1 chunk for short text, got %d", len(chunks))
		}
	})

	t.Run("default parameters", func(t *testing.T) {
		text := "测试文本"
		chunks := ChunkText(text, 0, 0)

		if len(chunks) == 0 {
			t.Error("expected at least one chunk")
		}
	})

	t.Run("medium length text", func(t *testing.T) {
		// Create text that fits in one chunk
		text := strings.Repeat("测", 100)
		chunks := ChunkText(text, 200, 20)

		if len(chunks) != 1 {
			t.Errorf("expected 1 chunk for medium text, got %d", len(chunks))
		}
	})

	t.Run("chunk index increments", func(t *testing.T) {
		text := strings.Repeat("测试内容", 100)
		chunks := ChunkText(text, 50, 10)

		for i, chunk := range chunks {
			if chunk.Index != i {
				t.Errorf("chunk %d has wrong index %d", i, chunk.Index)
			}
		}
	})
}

func TestChunkTextWithNewlines(t *testing.T) {
	t.Run("respects newline boundaries", func(t *testing.T) {
		text := "第一段内容\n第二段内容\n第三段内容\n第四段内容\n第五段内容"

		chunks := ChunkText(text, 20, 5)

		if len(chunks) == 0 {
			t.Error("expected at least one chunk")
		}
	})
}

func TestChunkTextTrimSpace(t *testing.T) {
	text := "  测试文本带有空格  \n\n"

	chunks := ChunkText(text, 100, 20)

	if len(chunks) == 0 {
		t.Error("expected at least one chunk")
	}
}

func TestChunkTextChinese(t *testing.T) {
	text := "这是一段中文测试文本，用于验证分块功能是否正常工作。"

	chunks := ChunkText(text, 50, 10)

	if len(chunks) == 0 {
		t.Error("expected at least one chunk")
	}
}

func TestChunkTextEnglish(t *testing.T) {
	text := "This is a test of the English text chunking functionality."

	chunks := ChunkText(text, 50, 10)

	if len(chunks) == 0 {
		t.Error("expected at least one chunk")
	}
}

func TestChunkTextMixed(t *testing.T) {
	text := "这是一段包含English and 中文 mixed content的测试文本。"

	chunks := ChunkText(text, 30, 5)

	if len(chunks) == 0 {
		t.Error("expected at least one chunk")
	}
}

func TestChunkConstants(t *testing.T) {
	if defaultChunkSize <= 0 {
		t.Error("defaultChunkSize should be positive")
	}
	if defaultChunkOverlap < 0 {
		t.Error("defaultChunkOverlap should not be negative")
	}
	if charsPerToken <= 0 {
		t.Error("charsPerToken should be positive")
	}

	if defaultChunkOverlap >= defaultChunkSize {
		t.Error("overlap should be smaller than chunk size")
	}
}

func TestChunkStruct(t *testing.T) {
	chunk := Chunk{
		Index: 5,
		Text:  "test content",
	}

	if chunk.Index != 5 {
		t.Error("index mismatch")
	}
	if chunk.Text != "test content" {
		t.Error("text mismatch")
	}
}


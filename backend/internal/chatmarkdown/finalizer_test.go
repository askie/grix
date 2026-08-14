package chatmarkdown

import "testing"

func TestRepairFinal(t *testing.T) {
	t.Run("normalizes ingress and keeps plain text", func(t *testing.T) {
		result := RepairFinal("\uFEFFhello\r\nworld\r")
		if result.Output != "hello\nworld" {
			t.Fatalf("output=%q want=%q", result.Output, "hello\nworld")
		}
		if result.ParsedWithGoldmark != true {
			t.Fatalf("parsed=%v want=true", result.ParsedWithGoldmark)
		}
		if result.HasStructuredMarkdown {
			t.Fatalf("has structured markdown = true want false")
		}
	})

	t.Run("adds missing space for ordered lists", func(t *testing.T) {
		result := RepairFinal("1.first item\n2.second item")
		want := "1. first item\n2. second item"
		if result.Output != want {
			t.Fatalf("output=%q want=%q", result.Output, want)
		}
		if !result.HasStructuredMarkdown {
			t.Fatalf("has structured markdown = false want true")
		}
	})

	t.Run("closes unclosed fenced code blocks", func(t *testing.T) {
		result := RepairFinal("```go\nfmt.Println(\"hi\")")
		want := "```go\nfmt.Println(\"hi\")\n```"
		if result.Output != want {
			t.Fatalf("output=%q want=%q", result.Output, want)
		}
		if !result.Changed {
			t.Fatalf("changed=false want true")
		}
		if !result.ParsedWithGoldmark {
			t.Fatalf("parsed=%v want=true", result.ParsedWithGoldmark)
		}
		if !result.HasStructuredMarkdown {
			t.Fatalf("has structured markdown = false want true")
		}
	})

	t.Run("does not mis-handle single-line fences", func(t *testing.T) {
		input := "```go fmt.Println(\"hi\") ```"
		result := RepairFinal(input)
		if result.Output != input {
			t.Fatalf("output=%q want=%q", result.Output, input)
		}
	})
}

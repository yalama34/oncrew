package rag

import (
	"strings"
	"testing"
)

func TestSplitMarkdownSectionsAndCode(t *testing.T) {
	text := "Введение\n# Магазин\nОбщее описание\n## Оплата\nКартой\n\n```sh\n# не заголовок\necho ok\n```\n## Доставка\nКурьером"
	chunks := SplitMarkdown(text)
	if len(chunks) != 4 {
		t.Fatalf("got %d chunks, want 4: %#v", len(chunks), chunks)
	}
	if chunks[0].Heading != "" || chunks[1].Heading != "Магазин" || chunks[2].Heading != "Магазин > Оплата" || chunks[3].Heading != "Магазин > Доставка" {
		t.Fatalf("unexpected headings: %#v", chunks)
	}
	if !strings.Contains(chunks[2].Content, "# не заголовок") {
		t.Fatalf("code block lost: %#v", chunks[2])
	}
}

func TestSplitMarkdownLongContent(t *testing.T) {
	text := "# Раздел\n" + strings.Repeat("я", 2500) + "\n\n" + strings.Repeat("а", 1800)
	chunks := SplitMarkdown(text)
	if len(chunks) < 2 {
		t.Fatalf("expected multiple chunks, got %d", len(chunks))
	}
	for _, chunk := range chunks {
		if chunk.Heading != "Раздел" || len([]rune(chunk.Content)) > 2000 {
			t.Fatalf("invalid chunk: heading=%q length=%d", chunk.Heading, len([]rune(chunk.Content)))
		}
	}
}

package rag

import (
	"regexp"
	"strings"
)

var headerRegex = regexp.MustCompile(`^ {0,3}#{1,6}[ \t]+`)

func hatchHeader(line string) bool {
	return headerRegex.MatchString(line)
}

func SplitMarkdown(text string) []Chunk {
	const maxChunkSize = 2000
	var chunks []Chunk
	var headings [6]string
	var section strings.Builder
	var fence byte
	fenceLength := 0

	flush := func() {
		var path []string
		for _, heading := range headings {
			if heading != "" {
				path = append(path, heading)
			}
		}
		heading := strings.Join(path, " > ")
		content := strings.TrimSpace(section.String())
		section.Reset()
		if content == "" {
			return
		}

		paragraphs := regexp.MustCompile(`\n[ \t]*\n+`).Split(content, -1)
		var current string
		for _, paragraph := range paragraphs {
			paragraph = strings.TrimSpace(paragraph)
			if paragraph == "" {
				continue
			}
			parts := []rune(paragraph)
			for len(parts) > 0 {
				size := min(len(parts), maxChunkSize)
				if size < len(parts) {
					for i := size; i > 0; i-- {
						if parts[i-1] == ' ' || parts[i-1] == '\n' || parts[i-1] == '\t' {
							size = i
							break
						}
					}
				}
				part := strings.TrimSpace(string(parts[:size]))
				parts = parts[size:]
				if part == "" {
					continue
				}
				if current != "" && len([]rune(current))+2+len([]rune(part)) > maxChunkSize {
					chunks = append(chunks, Chunk{Heading: heading, Content: current})
					current = ""
				}
				if current != "" {
					current += "\n\n"
				}
				current += part
			}
		}
		if current != "" {
			chunks = append(chunks, Chunk{Heading: heading, Content: current})
		}
	}

	text = strings.ReplaceAll(text, "\r\n", "\n")
	for _, line := range strings.SplitAfter(text, "\n") {
		trimmed := strings.TrimLeft(line, " ")
		indent := len(line) - len(trimmed)
		if indent <= 3 && len(trimmed) > 0 && (trimmed[0] == '`' || trimmed[0] == '~') {
			marker := trimmed[0]
			count := 0
			for count < len(trimmed) && trimmed[count] == marker {
				count++
			}
			if count >= 3 {
				if fence == 0 {
					fence, fenceLength = marker, count
				} else if fence == marker && count >= fenceLength && strings.TrimSpace(trimmed[count:]) == "" {
					fence = 0
				}
				section.WriteString(line)
				continue
			}
		}

		if fence == 0 && hatchHeader(line) {
			flush()
			header := strings.TrimSpace(line)
			level := 0
			for level < len(header) && header[level] == '#' {
				level++
			}
			for i := level - 1; i < len(headings); i++ {
				headings[i] = ""
			}
			headings[level-1] = strings.TrimSpace(header[level:])
			continue
		}
		section.WriteString(line)
	}
	flush()
	return chunks
}

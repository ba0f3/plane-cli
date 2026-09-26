package markdown

import (
	"bytes"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/renderer/html"
)

var codeBlockPattern = regexp.MustCompile(`(?s)<pre><code([^>]*)>(.*?)</code></pre>`)

// RenderHTML converts Markdown text to HTML.
// If the input already appears to be HTML (starts with "<"), it is returned unchanged.
func RenderHTML(input string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(input, "\r\n", "\n"))
	if normalized == "" {
		return ""
	}

	if strings.HasPrefix(normalized, "<") {
		return normalized
	}

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			extension.Strikethrough,
			extension.Linkify,
			extension.TaskList,
		),
		goldmark.WithRendererOptions(
			html.WithHardWraps(),
			html.WithXHTML(),
		),
	)

	var buf bytes.Buffer
	if err := md.Convert([]byte(normalized), &buf); err != nil {
		return "<p>" + normalized + "</p>"
	}

	return normalizeCodeBlockBlankLines(buf.String())
}

func normalizeCodeBlockBlankLines(rendered string) string {
	return codeBlockPattern.ReplaceAllStringFunc(rendered, func(block string) string {
		matches := codeBlockPattern.FindStringSubmatch(block)
		if len(matches) != 3 {
			return block
		}

		lines := strings.Split(matches[2], "\n")
		for i := 0; i < len(lines)-1; i++ {
			if lines[i] == "" {
				lines[i] = "<br />"
			}
		}

		return "<pre><code" + matches[1] + ">" + strings.Join(lines, "\n") + "</code></pre>"
	})
}

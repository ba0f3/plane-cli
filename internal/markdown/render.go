package markdown

import (
	"bytes"
	stdhtml "html"
	"regexp"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	goldmarkhtml "github.com/yuin/goldmark/renderer/html"
)

var codeBlockPattern = regexp.MustCompile(`(?s)<pre><code([^>]*)>(.*?)</code></pre>`)

// RenderHTML converts Markdown text to HTML. Raw HTML is not treated as an
// escape hatch; callers that intentionally accept raw HTML should bypass this
// function and send that HTML explicitly.
func RenderHTML(input string) string {
	normalized := normalize(input)
	if normalized == "" {
		return ""
	}

	md := goldmark.New(
		goldmark.WithExtensions(
			extension.Table,
			extension.Strikethrough,
			extension.Linkify,
			extension.TaskList,
		),
		goldmark.WithRendererOptions(
			goldmarkhtml.WithHardWraps(),
			goldmarkhtml.WithXHTML(),
		),
	)

	var buf bytes.Buffer
	if err := md.Convert([]byte(normalized), &buf); err != nil {
		return "<p>" + stdhtml.EscapeString(normalized) + "</p>"
	}

	return normalizeCodeBlockBlankLines(buf.String())
}

// RenderHTMLCompat preserves the historical issue/comment behavior where an
// input beginning with '<' is assumed to already be HTML.
func RenderHTMLCompat(input string) string {
	normalized := normalize(input)
	if normalized == "" {
		return ""
	}
	if strings.HasPrefix(normalized, "<") {
		return normalized
	}
	return RenderHTML(normalized)
}

func normalize(input string) string {
	return strings.TrimSpace(strings.ReplaceAll(input, "\r\n", "\n"))
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

package web

import (
	"bytes"
	"html/template"

	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
)

// Markdown is rendered in two layers, so that a mistake in either one alone
// cannot let script through: the renderer drops raw HTML (goldmark's default
// without html.WithUnsafe), and the output then passes an allow-list
// sanitizer.

var (
	md = goldmark.New(goldmark.WithExtensions(
		extension.Table, extension.Strikethrough, extension.Linkify, extension.TaskList,
	))
	policy = func() *bluemonday.Policy {
		p := bluemonday.UGCPolicy()
		p.AllowURLSchemes("http", "https", "mailto")
		p.RequireNoFollowOnLinks(true)
		p.AddTargetBlankToFullyQualifiedLinks(true)
		// Task list checkboxes.
		p.AllowAttrs("type").Matching(bluemonday.SpaceSeparatedTokens).OnElements("input")
		p.AllowAttrs("checked", "disabled").OnElements("input")
		return p
	}()
)

// renderMarkdown returns sanitized HTML for markdown source.
func renderMarkdown(src string) template.HTML {
	var buf bytes.Buffer
	if err := md.Convert([]byte(src), &buf); err != nil {
		// Fall back to the source as plain text.
		return template.HTML(template.HTMLEscapeString(src))
	}
	return template.HTML(policy.SanitizeBytes(buf.Bytes()))
}

package web

import (
	"regexp"
	"strings"
	"testing"
)

func TestMarkdownRenders(t *testing.T) {
	got := string(renderMarkdown("**bold** and [a link](https://example.com)\n\n- [x] done"))
	for _, want := range []string{"<strong>bold</strong>", `href="https://example.com"`, `rel="nofollow`, "<li>", "checkbox"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in %s", want, got)
		}
	}
}

var tagRe = regexp.MustCompile(`<[^>]*>`)

func TestMarkdownSanitizes(t *testing.T) {
	for _, src := range []string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(1)>`,
		`[click](javascript:alert(1))`,
		`[click](JaVaScRiPt:alert(1))`,
		`[click](data:text/html;base64,PHNjcmlwdD5hbGVydCgxKTwvc2NyaXB0Pg==)`,
		`<a href="javascript:alert(1)">x</a>`,
		"<iframe src=https://evil.example></iframe>",
		`![x](https://example.com/a.png" onerror="alert(1))`,
		"<div style=\"background:url(javascript:alert(1))\">x</div>",
		"<svg><script>alert(1)</script></svg>",
	} {
		got := strings.ToLower(string(renderMarkdown(src)))
		// Only markup matters: escaped text such as "onerror=" in a paragraph is harmless.
		tags := strings.Join(tagRe.FindAllString(got, -1), " ")
		for _, bad := range []string{"<script", "javascript:", " on", "<iframe", "data:", "style=", "<svg"} {
			if strings.Contains(tags, bad) {
				t.Errorf("%q rendered as %q, which contains %q", src, got, bad)
			}
		}
	}
}

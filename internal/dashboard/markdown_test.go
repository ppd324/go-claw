package dashboard

import (
	"strings"
	"testing"
)

func TestRenderDashboardMarkdownSupportsGFM(t *testing.T) {
	html := renderDashboardMarkdown("# 标题\n\n- 项目\n\n`code`\n\n| A | B |\n|---|---|\n| 1 | 2 |")
	for _, expected := range []string{"<h1>标题</h1>", "<li>项目</li>", "<code>code</code>", "<table>"} {
		if !strings.Contains(html, expected) {
			t.Fatalf("rendered markdown missing %q:\n%s", expected, html)
		}
	}
}

func TestRenderDashboardMarkdownDoesNotAllowRawHTML(t *testing.T) {
	html := renderDashboardMarkdown(`<script>alert("xss")</script><img src=x onerror=alert(1)>`)
	if strings.Contains(html, "<script") || strings.Contains(html, "<img") || strings.Contains(html, "onerror") {
		t.Fatalf("unsafe raw HTML was rendered: %s", html)
	}
}

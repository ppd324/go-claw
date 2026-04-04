package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type WebSearchTool struct {
	client *http.Client
}

func NewWebSearchTool() *WebSearchTool {
	return &WebSearchTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *WebSearchTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "web_search",
		Description: "Search the web for information. Returns search results with titles, URLs, and snippets. Use this when you need to find current information or research topics online.",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"query": {
					Type:        String,
					Description: "The search query",
				},
				"num": {
					Type:        Number,
					Description: "Number of results to return (default: 5, max: 10)",
					Default:     5,
				},
			},
			Required: []string{"query"},
		},
	}, nil
}

func (t *WebSearchTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p struct {
		Query string  `json:"query"`
		Num   float64 `json:"num"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	if p.Query == "" {
		return nil, fmt.Errorf("query is required")
	}

	num := int(p.Num)
	if num == 0 {
		num = 5
	}
	if num > 10 {
		num = 10
	}

	results, err := t.searchDuckDuckGo(ctx, p.Query, num)
	if err != nil {
		return nil, err
	}

	if len(results) == 0 {
		return &ToolResult{Text: "No results found for: " + p.Query}, nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Search results for: %s\n\n", p.Query))

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. %s\n", i+1, r.Title))
		sb.WriteString(fmt.Sprintf("   URL: %s\n", r.URL))
		sb.WriteString(fmt.Sprintf("   %s\n\n", r.Snippet))
	}

	return &ToolResult{Text: sb.String()}, nil
}

type SearchResult struct {
	Title   string
	URL     string
	Snippet string
}

func (t *WebSearchTool) searchDuckDuckGo(ctx context.Context, query string, num int) ([]SearchResult, error) {
	searchURL := fmt.Sprintf("https://html.duckduckgo.com/html/?q=%s", url.QueryEscape(query))

	req, err := http.NewRequestWithContext(ctx, "GET", searchURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.5")

	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch search results: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("search returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return t.parseDuckDuckGoResults(string(body), num), nil
}

func (t *WebSearchTool) parseDuckDuckGoResults(html string, num int) []SearchResult {
	var results []SearchResult

	resultRegex := regexp.MustCompile(`<a[^>]*class="result__a"[^>]*href="([^"]+)"[^>]*>([^<]+)</a>`)
	snippetRegex := regexp.MustCompile(`<a[^>]*class="result__snippet"[^>]*>([^<]+)</a>`)

	resultMatches := resultRegex.FindAllStringSubmatch(html, -1)
	snippetMatches := snippetRegex.FindAllStringSubmatch(html, -1)

	for i, match := range resultMatches {
		if i >= num {
			break
		}

		resultURL := match[1]
		title := strings.TrimSpace(match[2])

		if strings.Contains(resultURL, "duckduckgo.com") {
			if uddg := extractUDDG(resultURL); uddg != "" {
				resultURL = uddg
			}
		}

		snippet := ""
		if i < len(snippetMatches) {
			snippet = strings.TrimSpace(snippetMatches[i][1])
		}

		if resultURL != "" && title != "" {
			results = append(results, SearchResult{
				Title:   title,
				URL:     resultURL,
				Snippet: snippet,
			})
		}
	}

	return results
}

func extractUDDG(redirectURL string) string {
	u, err := url.Parse(redirectURL)
	if err != nil {
		return ""
	}
	uddg := u.Query().Get("uddg")
	if uddg != "" {
		return uddg
	}
	return ""
}

type WebFetchTool struct {
	client *http.Client
}

func NewWebFetchTool() *WebFetchTool {
	return &WebFetchTool{
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (t *WebFetchTool) Info(ctx context.Context) (*ToolInfo, error) {
	return &ToolInfo{
		Name:        "web_fetch",
		Description: "Fetch and extract text content from a web page. Use this to read the content of a URL found through web_search.",
		Parameters: ToolParameters{
			Type: Object,
			Properties: map[string]ToolParameter{
				"url": {
					Type:        String,
					Description: "The URL to fetch",
				},
				"max_length": {
					Type:        Number,
					Description: "Maximum characters to return (default: 5000)",
					Default:     5000,
				},
			},
			Required: []string{"url"},
		},
	}, nil
}

func (t *WebFetchTool) Invoke(ctx context.Context, params json.RawMessage, opt ...Option) (*ToolResult, error) {
	var p struct {
		URL       string  `json:"url"`
		MaxLength float64 `json:"max_length"`
	}

	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("failed to parse parameters: %w", err)
	}

	if p.URL == "" {
		return nil, fmt.Errorf("url is required")
	}

	maxLength := int(p.MaxLength)
	if maxLength == 0 {
		maxLength = 5000
	}

	content, err := t.fetchURL(ctx, p.URL, maxLength)
	if err != nil {
		return nil, err
	}

	return &ToolResult{Text: content}, nil
}

func (t *WebFetchTool) fetchURL(ctx context.Context, targetURL string, maxLength int) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", targetURL, nil)
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := t.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("fetch returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	content := t.extractText(string(body))

	if len(content) > maxLength {
		content = content[:maxLength] + "..."
	}

	return fmt.Sprintf("Content from %s:\n\n%s", targetURL, content), nil
}

func (t *WebFetchTool) extractText(html string) string {
	html = regexp.MustCompile(`<script[^>]*>[\s\S]*?</script>`).ReplaceAllString(html, "")
	html = regexp.MustCompile(`<style[^>]*>[\s\S]*?</style>`).ReplaceAllString(html, "")
	html = regexp.MustCompile(`<nav[^>]*>[\s\S]*?</nav>`).ReplaceAllString(html, "")
	html = regexp.MustCompile(`<footer[^>]*>[\s\S]*?</footer>`).ReplaceAllString(html, "")
	html = regexp.MustCompile(`<header[^>]*>[\s\S]*?</header>`).ReplaceAllString(html, "")

	html = regexp.MustCompile(`<br\s*/?>`).ReplaceAllString(html, "\n")
	html = regexp.MustCompile(`<p[^>]*>`).ReplaceAllString(html, "\n")
	html = regexp.MustCompile(`</p>`).ReplaceAllString(html, "\n")
	html = regexp.MustCompile(`<h[1-6][^>]*>`).ReplaceAllString(html, "\n")
	html = regexp.MustCompile(`</h[1-6]>`).ReplaceAllString(html, "\n")
	html = regexp.MustCompile(`<li[^>]*>`).ReplaceAllString(html, "\n- ")
	html = regexp.MustCompile(`<div[^>]*>`).ReplaceAllString(html, "\n")

	html = regexp.MustCompile(`<[^>]+>`).ReplaceAllString(html, "")

	html = regexp.MustCompile(`&nbsp;`).ReplaceAllString(html, " ")
	html = regexp.MustCompile(`&amp;`).ReplaceAllString(html, "&")
	html = regexp.MustCompile(`&lt;`).ReplaceAllString(html, "<")
	html = regexp.MustCompile(`&gt;`).ReplaceAllString(html, ">")
	html = regexp.MustCompile(`&quot;`).ReplaceAllString(html, "\"")
	html = regexp.MustCompile(`&#39;`).ReplaceAllString(html, "'")

	html = regexp.MustCompile(`\s+`).ReplaceAllString(html, " ")

	lines := strings.Split(html, "\n")
	var result []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}

	return strings.Join(result, "\n")
}

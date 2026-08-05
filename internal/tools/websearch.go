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

// webClient is shared across all web tools — one instance, no duplication.
var webClient = &http.Client{
	Timeout: 15 * time.Second,
	Transport: &http.Transport{
		MaxIdleConnsPerHost: 4,
	},
}

// ─── web_search ───────────────────────────────────────────────────────────────

// WebSearch searches the internet. Provider is selected by Provider field.
// Supported: "duckduckgo" (no key), "brave" (APIKey), "searxng" (BaseURL).
type WebSearch struct {
	Provider string // "duckduckgo" | "brave" | "searxng"
	APIKey   string // Brave
	BaseURL  string // SearXNG
}

func (t *WebSearch) Name() string { return "web_search" }
func (t *WebSearch) Description() string {
	return "Search the web for current information. Returns titles, URLs and snippets."
}
func (t *WebSearch) Schema() ParameterSchema {
	return NewSchema([]string{"query"}, map[string]Property{
		"query":       {Type: "string", Description: "Search query"},
		"num_results": {Type: "integer", Description: "Results to return (default 5, max 10)"},
	})
}

func (t *WebSearch) Execute(ctx context.Context, raw json.RawMessage) Result {
	var a struct {
		Query      string `json:"query"`
		NumResults int    `json:"num_results"`
	}
	if err := unmarshal(raw, &a); err != nil {
		return Errorf("invalid args: %v", err)
	}
	if a.NumResults <= 0 || a.NumResults > 10 {
		a.NumResults = 5
	}
	switch t.Provider {
	case "brave":
		return t.brave(ctx, a.Query, a.NumResults)
	case "searxng":
		return t.searxng(ctx, a.Query, a.NumResults)
	default:
		return t.duckduckgo(ctx, a.Query, a.NumResults)
	}
}

func (t *WebSearch) duckduckgo(ctx context.Context, query string, n int) Result {
	u := "https://api.duckduckgo.com/?q=" + url.QueryEscape(query) +
		"&format=json&no_html=1&skip_disambig=1"
	body, err := httpGet(ctx, u, nil)
	if err != nil {
		return Errorf("DDG: %v", err)
	}
	var d struct {
		Answer        string `json:"Answer"`
		AbstractText  string `json:"AbstractText"`
		AbstractURL   string `json:"AbstractURL"`
		RelatedTopics []struct {
			Text     string `json:"Text"`
			FirstURL string `json:"FirstURL"`
		} `json:"RelatedTopics"`
	}
	_ = json.Unmarshal(body, &d)

	var sb strings.Builder
	count := 0
	if d.Answer != "" {
		fmt.Fprintf(&sb, "Answer: %s\n\n", d.Answer)
		count++
	}
	if d.AbstractText != "" {
		fmt.Fprintf(&sb, "%s\n%s\n\n", d.AbstractText, d.AbstractURL)
		count++
	}
	for _, r := range d.RelatedTopics {
		if count >= n {
			break
		}
		if r.Text != "" {
			fmt.Fprintf(&sb, "- %s\n  %s\n\n", r.Text, r.FirstURL)
			count++
		}
	}
	if count == 0 {
		sb.WriteString("No instant results — try web_fetch with a direct URL.")
	}
	return Result{Content: sb.String()}
}

func (t *WebSearch) brave(ctx context.Context, query string, n int) Result {
	if t.APIKey == "" {
		return Errorf("Brave requires api_key in config.yaml tools.web_search.api_key")
	}
	u := fmt.Sprintf("https://api.search.brave.com/res/v1/web/search?q=%s&count=%d&text_decorations=false",
		url.QueryEscape(query), n)
	body, err := httpGet(ctx, u, map[string]string{"X-Subscription-Token": t.APIKey})
	if err != nil {
		return Errorf("Brave: %v", err)
	}
	var r struct {
		Web struct {
			Results []struct {
				Title       string `json:"title"`
				URL         string `json:"url"`
				Description string `json:"description"`
			} `json:"results"`
		} `json:"web"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Errorf("parse: %v", err)
	}
	var sb strings.Builder
	for i, res := range r.Web.Results {
		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n\n", i+1, res.Title, res.Description, res.URL)
	}
	if sb.Len() == 0 {
		sb.WriteString("No results.")
	}
	return Result{Content: sb.String()}
}

func (t *WebSearch) searxng(ctx context.Context, query string, n int) Result {
	if t.BaseURL == "" {
		return Errorf("SearXNG requires base_url in config.yaml tools.web_search.base_url")
	}
	u := strings.TrimRight(t.BaseURL, "/") + "/search?q=" + url.QueryEscape(query) + "&format=json"
	body, err := httpGet(ctx, u, nil)
	if err != nil {
		return Errorf("SearXNG: %v", err)
	}
	var r struct {
		Results []struct {
			Title   string `json:"title"`
			URL     string `json:"url"`
			Content string `json:"content"`
		} `json:"results"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return Errorf("parse: %v", err)
	}
	var sb strings.Builder
	for i, res := range r.Results {
		if i >= n {
			break
		}
		fmt.Fprintf(&sb, "%d. %s\n   %s\n   %s\n\n", i+1, res.Title, res.Content, res.URL)
	}
	if sb.Len() == 0 {
		sb.WriteString("No results.")
	}
	return Result{Content: sb.String()}
}

// ─── web_fetch ────────────────────────────────────────────────────────────────

// WebFetch fetches a URL and returns its text content.
type WebFetch struct{}

func (t WebFetch) Name() string { return "web_fetch" }
func (t WebFetch) Description() string {
	return "Fetch text content from a URL. Strips HTML. Use after web_search to read full articles."
}
func (t WebFetch) Schema() ParameterSchema {
	return NewSchema([]string{"url"}, map[string]Property{
		"url":       {Type: "string", Description: "URL to fetch (http/https)"},
		"max_bytes": {Type: "integer", Description: "Max bytes to return (default 16384)"},
	})
}
func (t WebFetch) Execute(ctx context.Context, raw json.RawMessage) Result {
	var a struct {
		URL      string `json:"url"`
		MaxBytes int    `json:"max_bytes"`
	}
	if err := unmarshal(raw, &a); err != nil {
		return Errorf("invalid args: %v", err)
	}
	if !strings.HasPrefix(a.URL, "http") {
		return Errorf("URL must start with http:// or https://")
	}
	if a.MaxBytes <= 0 {
		a.MaxBytes = 16384
	}
	body, err := httpGet(ctx, a.URL, map[string]string{
		"User-Agent": "Mozilla/5.0 (compatible; VoiceAssistant/1.0)",
		"Accept":     "text/html,text/plain,application/json",
	})
	if err != nil {
		return Errorf("%v", err)
	}
	text := stripHTML(string(body))
	runes := []rune(text)
	suffix := ""
	if len(runes) > a.MaxBytes {
		runes = runes[:a.MaxBytes]
		suffix = "\n[truncated]"
	}
	return Result{Content: fmt.Sprintf("Content from %s:\n\n%s%s", a.URL, string(runes), suffix)}
}

// ─── Shared HTTP helper ───────────────────────────────────────────────────────

func httpGet(ctx context.Context, rawURL string, headers map[string]string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := webClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 512*1024))
}

// ─── HTML stripping ───────────────────────────────────────────────────────────

var (
	reScript   = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>`)
	reStyle    = regexp.MustCompile(`(?is)<style[^>]*>.*?</style>`)
	reTag      = regexp.MustCompile(`<[^>]+>`)
	reSpaces   = regexp.MustCompile(`[ \t]{2,}`)
	reNewlines = regexp.MustCompile(`\n{3,}`)
	htmlEnts   = strings.NewReplacer(
		"&amp;", "&", "&lt;", "<", "&gt;", ">",
		"&quot;", `"`, "&#39;", "'", "&nbsp;", " ",
	)
)

func stripHTML(s string) string {
	s = reScript.ReplaceAllString(s, " ")
	s = reStyle.ReplaceAllString(s, " ")
	s = reTag.ReplaceAllString(s, " ")
	s = htmlEnts.Replace(s)
	s = reSpaces.ReplaceAllString(s, " ")
	s = reNewlines.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

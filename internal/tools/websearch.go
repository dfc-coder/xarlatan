package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// ─── web_search ───────────────────────────────────────────────────────────────

// WebSearch searches the internet. Provider is selected by Provider field.
// Supported: "duckduckgo" (no key), "brave" (APIKey), "searxng" (BaseURL).
type WebSearch struct {
	Provider string          // "duckduckgo" | "brave" | "searxng"
	APIKey   string          // Brave
	BaseURL  string          // SearXNG
	Client   *SafeHTTPClient // optional injection; secure shared default otherwise
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
	body, err := t.httpGet(ctx, u, nil)
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
	if err := json.Unmarshal(body, &d); err != nil {
		return Errorf("DDG parse: %v", err)
	}

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
	body, err := t.httpGet(ctx, u, map[string]string{"X-Subscription-Token": t.APIKey})
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
	body, err := t.httpGet(ctx, u, nil)
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

func (t *WebSearch) httpGet(
	ctx context.Context,
	rawURL string,
	headers map[string]string,
) ([]byte, error) {
	response, err := t.httpClient().Get(ctx, rawURL, headers, 0)
	if err != nil {
		return nil, err
	}
	if response.Truncated {
		return nil, &HTTPError{
			Code: HTTPBodyLimitExceeded,
			URL:  response.FinalURL,
			Err:  fmt.Errorf("structured response exceeded %d bytes", t.httpClient().config.MaxBodyBytes),
		}
	}
	return response.Body, nil
}

func (t *WebSearch) httpClient() *SafeHTTPClient {
	if t != nil && t.Client != nil {
		return t.Client
	}
	return defaultSafeHTTPClient
}

// ─── web_fetch ────────────────────────────────────────────────────────────────

// WebFetch fetches a URL and returns its text content.
type WebFetch struct {
	Client *SafeHTTPClient // optional injection; secure shared default otherwise
}

func (t WebFetch) Name() string { return "web_fetch" }
func (t WebFetch) Description() string {
	return "Fetch bounded text content from a public HTTP(S) URL. Private and local destinations are blocked."
}
func (t WebFetch) Schema() ParameterSchema {
	return NewSchema([]string{"url"}, map[string]Property{
		"url":       {Type: "string", Description: "Public URL to fetch (http/https)"},
		"max_bytes": {Type: "integer", Description: "Max response bytes to return (default 16384, hard max 524288)"},
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
	if a.MaxBytes <= 0 {
		a.MaxBytes = 16384
	}
	response, err := t.httpClient().Get(ctx, a.URL, map[string]string{
		"Accept": "text/html,text/plain,application/json,application/xml;q=0.9",
	}, int64(a.MaxBytes))
	if err != nil {
		return Errorf("%v", err)
	}
	text := stripHTML(string(response.Body))
	suffix := ""
	if response.Truncated {
		suffix = "\n[truncated]"
	}
	return Result{Content: fmt.Sprintf("Content from %s:\n\n%s%s", response.FinalURL, text, suffix)}
}

func (t WebFetch) httpClient() *SafeHTTPClient {
	if t.Client != nil {
		return t.Client
	}
	return defaultSafeHTTPClient
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

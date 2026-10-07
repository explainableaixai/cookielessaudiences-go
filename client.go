// Package cookielessaudiences is a client for the Cookieless Audiences API:
// page-level audience segmentation and IAB content categorization.
package cookielessaudiences

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// Version of this module.
	Version = "1.0.0"
	baseURL = "https://www.cookielessaudiences.com"
)

var statusText = map[int]string{
	400: "bad request, check the parameters",
	401: "invalid API key",
	403: "key not active or monthly credits used up",
	407: "missing data_type, must be url or text",
	410: "not enough content in the page or text",
	411: "the URL content could not be fetched",
	500: "general error, check the request or contact support",
}

// APIError is returned when the JSON body carries a status other than 200.
type APIError struct {
	Status  int
	Message string
	Body    map[string]any
}

func (e *APIError) Error() string {
	return fmt.Sprintf("cookielessaudiences: status %d: %s", e.Status, e.Message)
}

// Client calls the API with one key.
type Client struct {
	APIKey string
	HTTP   *http.Client
	Base   string
}

// New returns a client with a 120 second timeout.
func New(apiKey string) *Client {
	return &Client{APIKey: apiKey, HTTP: &http.Client{Timeout: 120 * time.Second}, Base: baseURL}
}

func (c *Client) post(ctx context.Context, path string, form url.Values) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+path, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "cookielessaudiences-go/"+Version+" (+https://www.cookielessaudiences.com)")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, err
	}
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		return nil, fmt.Errorf("cookielessaudiences: response was not a JSON object: %w", err)
	}
	if s, ok := body["status"].(float64); ok && int(s) != 200 {
		msg := statusText[int(s)]
		if msg == "" {
			msg = "API error"
		}
		return nil, &APIError{Status: int(s), Message: msg, Body: body}
	}
	return body, nil
}

// Segment returns structured audience segmentation for a URL.
func (c *Client) Segment(ctx context.Context, pageURL string) (map[string]any, error) {
	return c.post(ctx, "/api/audience/segment.php", url.Values{"query": {pageURL}, "api_key": {c.APIKey}, "format": {"structured"}})
}

// SegmentLegacy returns the legacy free-text shape.
func (c *Client) SegmentLegacy(ctx context.Context, pageURL string) (map[string]any, error) {
	return c.post(ctx, "/api/audience/segment.php", url.Values{"query": {pageURL}, "api_key": {c.APIKey}})
}

// Categorize returns IAB content categories with confidence scores for a URL.
func (c *Client) Categorize(ctx context.Context, pageURL string) (map[string]any, error) {
	return c.post(ctx, "/api/iab/iab_web_content_filtering.php",
		url.Values{"query": {pageURL}, "api_key": {c.APIKey}, "data_type": {"url"}, "confidence": {"1"}})
}

// CategorizeText returns IAB content categories for plain text.
func (c *Client) CategorizeText(ctx context.Context, text string) (map[string]any, error) {
	return c.post(ctx, "/api/iab/iab_content_filtering.php",
		url.Values{"query": {text}, "api_key": {c.APIKey}, "data_type": {"text"}, "confidence": {"1"}})
}

// Vocabularies fetches the public controlled vocabularies (no key needed).
func Vocabularies(ctx context.Context) (map[string]any, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/api/audience/filters.php", nil)
	if err != nil {
		return nil, err
	}
	res, err := (&http.Client{Timeout: 60 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	var out map[string]any
	return out, json.NewDecoder(res.Body).Decode(&out)
}

// Labels returns readable labels for the INT.* and PI.* codes of a structured response.
func Labels(result map[string]any) []string {
	names, _ := result["labels"].(map[string]any)
	var out []string
	for _, group := range []string{"interests", "purchase_intent"} {
		block, _ := result[group].(map[string]any)
		for _, key := range []string{"tier1", "tier2", "codes"} {
			list, _ := block[key].([]any)
			for _, v := range list {
				code, _ := v.(string)
				if label, ok := names[code].(string); ok {
					out = append(out, label)
				} else {
					out = append(out, code)
				}
			}
		}
	}
	return out
}

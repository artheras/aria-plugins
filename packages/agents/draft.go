// Package agents provides replaceable draft generation without payment authority.
package agents

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/artheras/aria-plugins/packages/sdk"
)

type Draft struct {
	Items   []sdk.Selection `json:"items"`
	Summary string          `json:"summary"`
}
type Drafter interface {
	Draft(context.Context, string, []sdk.Product) (Draft, error)
	Name() string
}
type Demo struct{}

func (Demo) Name() string { return "Local demo parser (no AI)" }
func (Demo) Draft(_ context.Context, prompt string, catalog []sdk.Product) (Draft, error) {
	// Deliberately bounded demo grammar. It does not impersonate an LLM.
	p := strings.ToLower(prompt)
	r := regexp.MustCompile(`(?i)([0-9]{1,3})\s+(?:cotton\s+)?(tees?|t-shirts?|caps?|hats?|ceramic\s+mugs?|mugs?|TSH-001|HAT-002|MUG-003)\b`)
	matches := r.FindAllStringSubmatch(p, -1)
	d := Draft{Summary: "Draft created by the local demo parser. Review quantities and prices before approval."}
	for _, m := range matches {
		q, _ := strconv.ParseInt(m[1], 10, 64)
		sku := "TSH-001"
		if strings.Contains(m[2], "hat") || strings.Contains(m[2], "cap") {
			sku = "HAT-002"
		}
		if strings.Contains(m[2], "mug") {
			sku = "MUG-003"
		}
		d.Items = append(d.Items, sdk.Selection{SKU: sku, Quantity: q})
	}
	if len(d.Items) == 0 {
		return d, errors.New("demo parser expects quantities and catalogue names, e.g. 2 tees and 1 mug")
	}
	return d, nil
}

type Gateway struct {
	BaseURL, Key, Model string
	// KeyFunc, when set, supplies the bearer for each call instead of Key.
	KeyFunc func() (string, error)
	Client  *http.Client
}

func (g Gateway) Name() string { return "AI Gateway: " + g.Model }
func (g Gateway) Draft(ctx context.Context, prompt string, catalog []sdk.Product) (Draft, error) {
	catalogJSON, _ := json.Marshal(catalog)
	request := map[string]any{
		"model": g.Model, "temperature": 0, "max_tokens": 700,
		"response_format": map[string]string{"type": "json_object"},
		"messages": []map[string]string{
			{"role": "system", "content": "Create an order draft using only this catalogue: " + string(catalogJSON) + ". Return ONLY JSON with items [{sku,quantity}] and summary, where summary is one short plain-text sentence (a string, not an object). Quantities must be integers 1..100. Never invent SKUs or prices. If the request is ambiguous or unrelated, return items:[] with an explanation. Do not approve orders or call payment tools."},
			{"role": "user", "content": prompt},
		},
	}
	body, _ := json.Marshal(request)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(g.BaseURL, "/")+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Draft{}, errors.New("invalid AI endpoint")
	}
	key := g.Key
	if g.KeyFunc != nil {
		if key, err = g.KeyFunc(); err != nil {
			return Draft{}, err
		}
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	client := g.Client
	if client == nil {
		client = &http.Client{Timeout: 45 * time.Second}
	}
	res, err := client.Do(req)
	if err != nil {
		return Draft{}, errors.New("AI service unavailable")
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return Draft{}, fmt.Errorf("AI service rejected the request (HTTP %d); check model and credentials", res.StatusCode)
	}
	var envelope struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err = json.NewDecoder(io.LimitReader(res.Body, 128<<10)).Decode(&envelope); err != nil || len(envelope.Choices) != 1 {
		return Draft{}, errors.New("invalid AI response")
	}
	// Items are what matters and are validated strictly below. The summary is
	// commentary: a real model sometimes returns it as an object (Gemini on
	// Vertex answered {"total_amount": 6000}), and rejecting a correct order for
	// that would turn a harmless formatting choice into a failed draft.
	var raw struct {
		Items   []sdk.Selection `json:"items"`
		Summary json.RawMessage `json:"summary"`
	}
	decoder := json.NewDecoder(strings.NewReader(envelope.Choices[0].Message.Content))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&raw); err != nil {
		return Draft{}, errors.New("AI did not return a valid order draft")
	}
	draft := Draft{Items: raw.Items}
	if err = json.Unmarshal(raw.Summary, &draft.Summary); err != nil {
		draft.Summary = ""
	}
	if strings.TrimSpace(draft.Summary) == "" {
		draft.Summary = "Draft prepared from your request. Review quantities and prices before approval."
	}
	if err = decoder.Decode(&struct{}{}); err != io.EOF {
		return Draft{}, errors.New("unexpected trailing AI content")
	}
	if _, err = sdk.BuildInvoice("validation", draft.Items, catalog); err != nil {
		return Draft{}, err
	}
	if len(draft.Summary) > 600 {
		draft.Summary = draft.Summary[:600]
	}
	return draft, nil
}

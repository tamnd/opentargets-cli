// Package opentargets is the library behind the opentargets command line:
// the HTTP client, GraphQL request shaping, and typed data models for the
// Open Targets Platform — a disease-target association database covering
// 60,000+ drug targets and 30,000+ diseases.
//
// The Client posts GraphQL queries to the public API at
// api.platform.opentargets.org. No API key is required.
package opentargets

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// DefaultUserAgent identifies the client to the Open Targets API.
const DefaultUserAgent = "opentargets/dev (+https://github.com/tamnd/opentargets-cli)"

// Host is the API hostname and the host the URI driver in domain.go claims.
const Host = "api.platform.opentargets.org"

// graphqlURL is the single GraphQL endpoint.
const graphqlURL = "https://" + Host + "/api/v4/graphql"

// platformURL is the base URL for human-readable links.
const platformURL = "https://platform.opentargets.org"

// --- wire types (match the API JSON shapes exactly) ---

type wireTarget struct {
	ID     string `json:"id"`
	Symbol string `json:"approvedSymbol"`
	Name   string `json:"approvedName"`
	Bio    string `json:"biotype"`
}

type wireDisease struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type wireSearchHit struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type wireSearchResult struct {
	Total int             `json:"total"`
	Hits  []wireSearchHit `json:"hits"`
}

type wireAssocRow struct {
	Score   float64      `json:"score"`
	Disease *wireDisease `json:"disease,omitempty"`
	Target  *wireTarget  `json:"target,omitempty"`
}

// --- public record types ---

// Target is one gene/protein target from Open Targets.
type Target struct {
	ID      string `json:"id"      kit:"id"`
	Symbol  string `json:"symbol,omitempty"`
	Name    string `json:"name,omitempty"`
	Biotype string `json:"biotype,omitempty"`
}

// Disease is one disease/phenotype from Open Targets.
type Disease struct {
	ID          string `json:"id"          kit:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// SearchResult is a single hit from a keyword search.
type SearchResult struct {
	ID   string `json:"id"   kit:"id"`
	Name string `json:"name"`
}

// Association is a scored target-disease association.
type Association struct {
	TargetID     string  `json:"target_id,omitempty"`
	TargetSymbol string  `json:"target_symbol,omitempty"`
	DiseaseID    string  `json:"disease_id,omitempty"`
	DiseaseName  string  `json:"disease_name,omitempty"`
	Score        float64 `json:"score"`
}

// --- conversion helpers ---

func targetFromWire(w *wireTarget) *Target {
	return &Target{ID: w.ID, Symbol: w.Symbol, Name: w.Name, Biotype: w.Bio}
}

func diseaseFromWire(w *wireDisease) *Disease {
	return &Disease{ID: w.ID, Name: w.Name, Description: w.Description}
}

// --- client ---

// Client talks to the Open Targets GraphQL API.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	// BaseURL may be overridden in tests.
	BaseURL string
	// Rate is the minimum gap between requests. Zero means no pacing.
	Rate    time.Duration
	Retries int

	last time.Time
}

// NewClient returns a Client with sensible defaults: 30s timeout, 300ms pacing,
// three retries on transient errors.
func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 30 * time.Second},
		UserAgent: DefaultUserAgent,
		BaseURL:   graphqlURL,
		Rate:      300 * time.Millisecond,
		Retries:   3,
	}
}

// do posts a GraphQL query and JSON-decodes the response into result.
// The API returns {"data": <result>, "errors": [...]}.
func (c *Client) do(ctx context.Context, query string, variables map[string]interface{}, result interface{}) error {
	body, err := json.Marshal(map[string]interface{}{
		"query":     query,
		"variables": variables,
	})
	if err != nil {
		return fmt.Errorf("marshal graphql request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt <= c.Retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff(attempt)):
			}
		}

		c.pace()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.BaseURL, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("User-Agent", c.UserAgent)

		resp, err := c.HTTP.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
			_ = resp.Body.Close()
			lastErr = fmt.Errorf("http %d", resp.StatusCode)
			continue
		}
		if resp.StatusCode != http.StatusOK {
			_ = resp.Body.Close()
			return fmt.Errorf("http %d", resp.StatusCode)
		}

		respBody, err := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		if err != nil {
			lastErr = err
			continue
		}

		// Decode into {"data": result, "errors": [...]}
		var envelope struct {
			Data   json.RawMessage `json:"data"`
			Errors []struct {
				Message string `json:"message"`
			} `json:"errors"`
		}
		if err := json.Unmarshal(respBody, &envelope); err != nil {
			return fmt.Errorf("decode graphql envelope: %w", err)
		}
		if len(envelope.Errors) > 0 {
			return fmt.Errorf("graphql error: %s", envelope.Errors[0].Message)
		}
		if err := json.Unmarshal(envelope.Data, result); err != nil {
			return fmt.Errorf("decode graphql data: %w", err)
		}
		return nil
	}
	return fmt.Errorf("opentargets: %w", lastErr)
}

// SearchTargets searches targets by keyword and returns matching results plus
// the total hit count. limit ≤ 0 defaults to 10.
func (c *Client) SearchTargets(ctx context.Context, q string, limit int) ([]*SearchResult, int, error) {
	if limit <= 0 {
		limit = 10
	}
	const gql = `
query($q: String!, $size: Int!) {
  search(queryString: $q, entityNames: ["target"], page: {index: 0, size: $size}) {
    total
    hits { id name }
  }
}`
	var data struct {
		Search wireSearchResult `json:"search"`
	}
	if err := c.do(ctx, gql, map[string]interface{}{"q": q, "size": limit}, &data); err != nil {
		return nil, 0, err
	}
	out := make([]*SearchResult, 0, len(data.Search.Hits))
	for _, h := range data.Search.Hits {
		out = append(out, &SearchResult{ID: h.ID, Name: h.Name})
	}
	return out, data.Search.Total, nil
}

// GetTarget fetches a single target by Ensembl ID (e.g. "ENSG00000141510").
func (c *Client) GetTarget(ctx context.Context, ensemblID string) (*Target, error) {
	const gql = `
query($id: String!) {
  target(ensemblId: $id) { id approvedSymbol approvedName biotype }
}`
	var data struct {
		Target *wireTarget `json:"target"`
	}
	if err := c.do(ctx, gql, map[string]interface{}{"id": ensemblID}, &data); err != nil {
		return nil, err
	}
	if data.Target == nil {
		return nil, fmt.Errorf("target not found: %s", ensemblID)
	}
	return targetFromWire(data.Target), nil
}

// GetDisease fetches a single disease by EFO ID (e.g. "EFO_0000311").
func (c *Client) GetDisease(ctx context.Context, efoID string) (*Disease, error) {
	const gql = `
query($id: String!) {
  disease(efoId: $id) { id name description }
}`
	var data struct {
		Disease *wireDisease `json:"disease"`
	}
	if err := c.do(ctx, gql, map[string]interface{}{"id": efoID}, &data); err != nil {
		return nil, err
	}
	if data.Disease == nil {
		return nil, fmt.Errorf("disease not found: %s", efoID)
	}
	return diseaseFromWire(data.Disease), nil
}

// TargetDiseases returns the top diseases associated with a target, ordered by
// association score. limit ≤ 0 defaults to 10.
func (c *Client) TargetDiseases(ctx context.Context, ensemblID string, limit int) ([]*Association, error) {
	if limit <= 0 {
		limit = 10
	}
	const gql = `
query($id: String!, $size: Int!) {
  target(ensemblId: $id) {
    associatedDiseases(page: {index: 0, size: $size}) {
      count
      rows { disease { id name } score }
    }
  }
}`
	var data struct {
		Target *struct {
			AssociatedDiseases struct {
				Rows []wireAssocRow `json:"rows"`
			} `json:"associatedDiseases"`
		} `json:"target"`
	}
	if err := c.do(ctx, gql, map[string]interface{}{"id": ensemblID, "size": limit}, &data); err != nil {
		return nil, err
	}
	if data.Target == nil {
		return nil, fmt.Errorf("target not found: %s", ensemblID)
	}
	var out []*Association
	for _, r := range data.Target.AssociatedDiseases.Rows {
		a := &Association{Score: r.Score}
		if r.Disease != nil {
			a.DiseaseID = r.Disease.ID
			a.DiseaseName = r.Disease.Name
		}
		out = append(out, a)
	}
	return out, nil
}

// DiseaseTargets returns the top targets associated with a disease, ordered by
// association score. limit ≤ 0 defaults to 10.
func (c *Client) DiseaseTargets(ctx context.Context, efoID string, limit int) ([]*Association, error) {
	if limit <= 0 {
		limit = 10
	}
	const gql = `
query($id: String!, $size: Int!) {
  disease(efoId: $id) {
    associatedTargets(page: {index: 0, size: $size}) {
      count
      rows { target { id approvedSymbol } score }
    }
  }
}`
	var data struct {
		Disease *struct {
			AssociatedTargets struct {
				Rows []wireAssocRow `json:"rows"`
			} `json:"associatedTargets"`
		} `json:"disease"`
	}
	if err := c.do(ctx, gql, map[string]interface{}{"id": efoID, "size": limit}, &data); err != nil {
		return nil, err
	}
	if data.Disease == nil {
		return nil, fmt.Errorf("disease not found: %s", efoID)
	}
	var out []*Association
	for _, r := range data.Disease.AssociatedTargets.Rows {
		a := &Association{Score: r.Score}
		if r.Target != nil {
			a.TargetID = r.Target.ID
			a.TargetSymbol = r.Target.Symbol
		}
		out = append(out, a)
	}
	return out, nil
}

// pace blocks until at least Rate has elapsed since the last request.
func (c *Client) pace() {
	if c.Rate <= 0 {
		return
	}
	if wait := c.Rate - time.Since(c.last); wait > 0 {
		time.Sleep(wait)
	}
	c.last = time.Now()
}

func backoff(attempt int) time.Duration {
	d := time.Duration(attempt) * 500 * time.Millisecond
	if d > 5*time.Second {
		d = 5 * time.Second
	}
	return d
}

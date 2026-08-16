package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	ErrDisabled = errors.New("gemini client is disabled")
	ErrNoResult = errors.New("gemini returned no usable metadata")
)

type Input struct {
	Title        string
	Filename     string
	RelativePath string
}

type Result struct {
	Title         string   `json:"title"`
	Authors       []string `json:"authors"`
	PublishedYear *int     `json:"published_year"`
	Description   string   `json:"description"`
}

type Client struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
	timeout    time.Duration
}

func NewClient(apiKey, model string, timeout time.Duration, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{}
	}
	if model == "" {
		model = "gemini-2.5-flash-lite"
	}
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	return &Client{
		apiKey:     strings.TrimSpace(apiKey),
		model:      strings.TrimSpace(model),
		baseURL:    "https://generativelanguage.googleapis.com/v1beta",
		httpClient: httpClient,
		timeout:    timeout,
	}
}

func (c *Client) Enabled() bool {
	return c != nil && c.apiKey != ""
}

func (c *Client) Model() string {
	if c == nil {
		return ""
	}
	return c.model
}

func (c *Client) Enrich(ctx context.Context, input Input) (Result, error) {
	if !c.Enabled() {
		return Result{}, ErrDisabled
	}

	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	payload := generateRequest{
		Contents: []content{{
			Role:  "user",
			Parts: []part{{Text: metadataPrompt(input)}},
		}},
		GenerationConfig: generationConfig{
			ResponseMIMEType: "application/json",
			ResponseSchema:   metadataSchema(),
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return Result{}, fmt.Errorf("encode gemini request: %w", err)
	}

	endpoint := strings.TrimRight(c.baseURL, "/") + "/models/" + url.PathEscape(c.model) + ":generateContent"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return Result{}, fmt.Errorf("create gemini request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("x-goog-api-key", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return Result{}, fmt.Errorf("call gemini: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 128<<10))
	if err != nil {
		return Result{}, fmt.Errorf("read gemini response: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return Result{}, fmt.Errorf("gemini response: status %d: %s", resp.StatusCode, compactError(responseBody))
	}

	var response generateResponse
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return Result{}, fmt.Errorf("decode gemini response: %w", err)
	}
	text, err := responseText(response)
	if err != nil {
		return Result{}, err
	}
	text = stripJSONFence(text)

	var result Result
	if err := json.Unmarshal([]byte(text), &result); err != nil {
		return Result{}, fmt.Errorf("decode gemini metadata JSON: %w", err)
	}
	return normalizeResult(result)
}

type generateRequest struct {
	Contents         []content        `json:"contents"`
	GenerationConfig generationConfig `json:"generationConfig"`
}

type content struct {
	Role  string `json:"role"`
	Parts []part `json:"parts"`
}

type part struct {
	Text string `json:"text"`
}

type generationConfig struct {
	ResponseMIMEType string         `json:"responseMimeType"`
	ResponseSchema   map[string]any `json:"responseSchema"`
}

type generateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
}

func metadataPrompt(input Input) string {
	return fmt.Sprintf(`Extraia metadados bibliográficos do livro abaixo.

Regras:
- Use somente as informações fornecidas e conhecimento bibliográfico confiável.
- Não invente autor, ano ou resumo quando não houver segurança.
- Retorne apenas o JSON solicitado, sem markdown.
- O título deve ser limpo, sem extensão ou nome do arquivo.
- O resumo deve ter no máximo 400 caracteres e ser vazio quando não for confiável.
- Use ano 0 quando desconhecido.

Título inferido: %s
Nome do arquivo: %s
Caminho relativo: %s`, limitText(input.Title, 300), limitText(input.Filename, 300), limitText(input.RelativePath, 500))
}

func metadataSchema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"title":          map[string]any{"type": "string"},
			"authors":        map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"published_year": map[string]any{"type": "integer", "minimum": 0, "maximum": 2100},
			"description":    map[string]any{"type": "string"},
		},
		"required":             []string{"title", "authors", "published_year", "description"},
		"additionalProperties": false,
	}
}

func responseText(response generateResponse) (string, error) {
	for _, candidate := range response.Candidates {
		for _, part := range candidate.Content.Parts {
			if strings.TrimSpace(part.Text) != "" {
				return part.Text, nil
			}
		}
	}
	return "", ErrNoResult
}

func normalizeResult(result Result) (Result, error) {
	result.Title = strings.TrimSpace(result.Title)
	result.Description = strings.TrimSpace(result.Description)
	if result.Title == "" {
		return Result{}, ErrNoResult
	}
	if len(result.Description) > 400 {
		result.Description = result.Description[:400]
	}
	if result.PublishedYear != nil && (*result.PublishedYear < 1000 || *result.PublishedYear > 2100) {
		result.PublishedYear = nil
	}
	if result.PublishedYear != nil && *result.PublishedYear == 0 {
		result.PublishedYear = nil
	}

	authors := make([]string, 0, len(result.Authors))
	seen := make(map[string]struct{}, len(result.Authors))
	for _, author := range result.Authors {
		author = strings.TrimSpace(author)
		if author == "" || len(authors) >= 8 {
			continue
		}
		key := strings.ToLower(author)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		authors = append(authors, author)
	}
	result.Authors = authors
	return result, nil
}

func stripJSONFence(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "```") && strings.HasSuffix(value, "```") {
		value = strings.TrimPrefix(value, "```")
		value = strings.TrimSpace(strings.TrimPrefix(value, "json"))
		value = strings.TrimSuffix(strings.TrimSpace(value), "```")
	}
	return strings.TrimSpace(value)
}

func compactError(body []byte) string {
	value := strings.Join(strings.Fields(string(body)), " ")
	if len(value) > 300 {
		return value[:300]
	}
	return value
}

func limitText(value string, max int) string {
	value = strings.TrimSpace(value)
	if len(value) > max {
		return value[:max]
	}
	return value
}

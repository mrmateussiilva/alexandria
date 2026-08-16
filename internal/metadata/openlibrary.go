package metadata

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var ErrNoMatch = errors.New("metadata match not found")

type SearchResult struct {
	ProviderKey   string
	Title         string
	Authors       []string
	PublishedYear *int
	CoverURL      *string
	SourceURL     *string
	Confidence    float64
}

type OpenLibraryClient struct {
	baseURL      string
	coverBaseURL string
	httpClient   *http.Client
	userAgent    string
}

func NewOpenLibraryClient(httpClient *http.Client) *OpenLibraryClient {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 12 * time.Second}
	}
	return &OpenLibraryClient{
		baseURL:      "https://openlibrary.org",
		coverBaseURL: "https://covers.openlibrary.org",
		httpClient:   httpClient,
		userAgent:    "Alexandria/0.1 (self-hosted personal library)",
	}
}

func (c *OpenLibraryClient) Search(ctx context.Context, title string) (SearchResult, error) {
	title = strings.TrimSpace(title)
	if title == "" {
		return SearchResult{}, ErrNoMatch
	}

	endpoint, err := url.Parse(c.baseURL + "/search.json")
	if err != nil {
		return SearchResult{}, fmt.Errorf("parse openlibrary endpoint: %w", err)
	}
	query := endpoint.Query()
	query.Set("title", title)
	query.Set("fields", "key,title,author_name,first_publish_year,cover_i")
	query.Set("limit", "1")
	endpoint.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint.String(), nil)
	if err != nil {
		return SearchResult{}, fmt.Errorf("create openlibrary request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return SearchResult{}, fmt.Errorf("search openlibrary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return SearchResult{}, fmt.Errorf("search openlibrary: status %d", resp.StatusCode)
	}

	var payload openLibrarySearchResponse
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return SearchResult{}, fmt.Errorf("decode openlibrary response: %w", err)
	}
	if len(payload.Docs) == 0 {
		return SearchResult{}, ErrNoMatch
	}

	doc := payload.Docs[0]
	if doc.Key == "" || strings.TrimSpace(doc.Title) == "" {
		return SearchResult{}, ErrNoMatch
	}

	var coverURL *string
	if doc.CoverID != nil {
		value := fmt.Sprintf("%s/b/id/%d-M.jpg", strings.TrimRight(c.coverBaseURL, "/"), *doc.CoverID)
		coverURL = &value
	}

	sourceURL := c.baseURL + doc.Key
	return SearchResult{
		ProviderKey:   doc.Key,
		Title:         doc.Title,
		Authors:       doc.AuthorNames,
		PublishedYear: doc.FirstPublishYear,
		CoverURL:      coverURL,
		SourceURL:     &sourceURL,
		Confidence:    titleConfidence(title, doc.Title),
	}, nil
}

type openLibrarySearchResponse struct {
	Docs []openLibraryDoc `json:"docs"`
}

type openLibraryDoc struct {
	Key              string   `json:"key"`
	Title            string   `json:"title"`
	AuthorNames      []string `json:"author_name"`
	FirstPublishYear *int     `json:"first_publish_year"`
	CoverID          *int     `json:"cover_i"`
}

func titleConfidence(query, result string) float64 {
	query = normalizeTitle(query)
	result = normalizeTitle(result)
	if query == "" || result == "" {
		return 0.4
	}
	if query == result {
		return 1
	}
	if strings.Contains(query, result) || strings.Contains(result, query) {
		return 0.82
	}

	queryWords := strings.Fields(query)
	resultWords := strings.Fields(result)
	if len(queryWords) == 0 || len(resultWords) == 0 {
		return 0.45
	}
	resultSet := make(map[string]struct{}, len(resultWords))
	for _, word := range resultWords {
		resultSet[word] = struct{}{}
	}
	matches := 0
	for _, word := range queryWords {
		if _, ok := resultSet[word]; ok {
			matches++
		}
	}
	return math.Max(0.45, float64(matches)/float64(len(queryWords)))
}

func normalizeTitle(value string) string {
	value = strings.ToLower(value)
	replacer := strings.NewReplacer(":", " ", ";", " ", ",", " ", ".", " ", "_", " ", "-", " ")
	value = replacer.Replace(value)
	return strings.Join(strings.Fields(value), " ")
}

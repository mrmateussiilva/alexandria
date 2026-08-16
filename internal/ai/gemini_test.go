package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientEnrichesMetadataWithStructuredJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-goog-api-key") != "test-key" {
			t.Fatalf("expected API key header")
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("read request: %v", err)
		}
		var request map[string]any
		if err := json.Unmarshal(body, &request); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if request["generationConfig"] == nil {
			t.Fatal("expected structured generation config")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"candidates\":[{\"content\":{\"parts\":[{\"text\":\"```json\\n{\\\"title\\\":\\\"Clean Code\\\",\\\"authors\\\":[\\\"Robert C. Martin\\\",\\\"Robert C. Martin\\\"],\\\"published_year\\\":2008,\\\"description\\\":\\\"Um livro sobre boas práticas de desenvolvimento.\\\"}\\n```\"}]}}]}"))
	}))
	defer server.Close()

	client := NewClient("test-key", "gemini-test", time.Second, server.Client())
	client.baseURL = server.URL
	result, err := client.Enrich(context.Background(), Input{
		Title:        "Clean Code",
		Filename:     "Clean Code - Robert C. Martin.pdf",
		RelativePath: "Programacao/Clean Code - Robert C. Martin.pdf",
	})
	if err != nil {
		t.Fatalf("enrich metadata: %v", err)
	}
	if result.Title != "Clean Code" || len(result.Authors) != 1 || result.PublishedYear == nil || *result.PublishedYear != 2008 {
		t.Fatalf("unexpected result: %#v", result)
	}
}

func TestClientRejectsEmptyMetadata(t *testing.T) {
	result, err := normalizeResult(Result{Title: "", Description: strings.Repeat("x", 500)})
	if err != ErrNoResult {
		t.Fatalf("expected ErrNoResult, got %v and %#v", err, result)
	}
}

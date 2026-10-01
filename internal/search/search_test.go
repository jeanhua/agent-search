package search

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestSearchBuildsQueryAndTruncates(t *testing.T) {
	var gotQuery, gotFormat, gotLanguage, gotUA string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotQuery = r.URL.Query().Get("q")
		gotFormat = r.URL.Query().Get("format")
		gotLanguage = r.URL.Query().Get("language")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"query": "golang",
			"results": []Result{
				{Title: "r1", URL: "https://go.dev/"},
				{Title: "r2", URL: "https://pkg.go.dev/"},
				{Title: "r3", URL: "https://blog.golang.org/"},
			},
		})
	}))
	defer srv.Close()

	c := NewClient(srv.URL, 5*time.Second)
	results, err := c.Search(context.Background(), "golang", 2, "zh-CN")
	if err != nil {
		t.Fatalf("search error: %v", err)
	}
	if gotQuery != "golang" || gotFormat != "json" || gotLanguage != "zh-CN" {
		t.Fatalf("unexpected query params: q=%q format=%q language=%q", gotQuery, gotFormat, gotLanguage)
	}
	if !strings.Contains(gotUA, "agent-search") {
		t.Errorf("User-Agent = %q", gotUA)
	}
	if len(results) != 2 || results[0].Title != "r1" {
		t.Fatalf("expected results truncated to 2, got %v", results)
	}
}

func TestSearchNoLanguageOmitsParam(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("language") != "" {
			t.Errorf("language should be omitted, got %q", r.URL.Query().Get("language"))
		}
		_, _ = w.Write([]byte(`{"query":"x","results":[]}`))
	}))
	defer srv.Close()

	if _, err := NewClient(srv.URL, 5*time.Second).Search(context.Background(), "x", 5, ""); err != nil {
		t.Fatalf("search error: %v", err)
	}
}

func TestSearchHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusForbidden)
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, 5*time.Second).Search(context.Background(), "x", 5, "")
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected 403 error, got %v", err)
	}
}

func TestSearchInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	}))
	defer srv.Close()

	_, err := NewClient(srv.URL, 5*time.Second).Search(context.Background(), "x", 5, "")
	if err == nil || !strings.Contains(err.Error(), "解析") {
		t.Fatalf("expected parse error, got %v", err)
	}
}

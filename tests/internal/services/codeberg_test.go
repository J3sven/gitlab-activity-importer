package services_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/furmanp/gitlab-activity-importer/internal/services"
)

func TestGetCodebergRepos(t *testing.T) {
	username := "testuser"
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != fmt.Sprintf("/api/v1/users/%s/repos", username) {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "token codeberg-token" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		page := r.URL.Query().Get("page")
		if page == "" || page == "1" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `[{"name":"repo","owner":{"login":"testuser"}}]`)
			return
		}
		fmt.Fprint(w, `[]`)
	}))
	defer mockServer.Close()

	os.Setenv("CODEBERG_BASE_URL", mockServer.URL)
	os.Setenv("CODEBERG_TOKEN", "codeberg-token")
	defer func() {
		_ = os.Unsetenv("CODEBERG_BASE_URL")
		_ = os.Unsetenv("CODEBERG_TOKEN")
	}()

	repos, err := services.GetCodebergRepos(username)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(repos) != 1 {
		t.Fatalf("expected one repo, got %d", len(repos))
	}
	if repos[0].Name != "repo" {
		t.Errorf("unexpected repo name: %s", repos[0].Name)
	}
	if repos[0].Owner.Login != username {
		t.Errorf("unexpected owner: %s", repos[0].Owner.Login)
	}
}

func TestGetCodebergRepoCommits(t *testing.T) {
	owner := "testuser"
	repo := "example"
	username := "testuser"
	mockServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		expectedPath := fmt.Sprintf("/api/v1/repos/%s/%s/commits", owner, repo)
		if r.URL.Path != expectedPath {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "token codeberg-token" {
			t.Fatalf("unexpected auth header: %s", r.Header.Get("Authorization"))
		}
		if r.URL.Query().Get("author") != username {
			t.Fatalf("unexpected author query: %s", r.URL.Query().Get("author"))
		}
		fmt.Fprint(w, `[{"sha":"abc123","message":"test","commit":{"author":{"name":"Author","email":"author@example.com","date":"2024-01-01T12:00:00Z"}}}]`)
	}))
	defer mockServer.Close()

	os.Setenv("CODEBERG_BASE_URL", mockServer.URL)
	os.Setenv("CODEBERG_TOKEN", "codeberg-token")
	defer func() {
		_ = os.Unsetenv("CODEBERG_BASE_URL")
		_ = os.Unsetenv("CODEBERG_TOKEN")
	}()

	commits, err := services.GetCodebergRepoCommits(owner, repo, username)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(commits) != 1 {
		t.Fatalf("expected one commit, got %d", len(commits))
	}
	if commits[0].ID != "abc123" {
		t.Errorf("unexpected commit ID: %s", commits[0].ID)
	}
	if commits[0].AuthorName != "Author" {
		t.Errorf("unexpected author name: %s", commits[0].AuthorName)
	}
	if commits[0].AuthorMail != "author@example.com" {
		t.Errorf("unexpected author email: %s", commits[0].AuthorMail)
	}
	if commits[0].AuthoredDate.IsZero() {
		t.Error("expected authored date to be set")
	}
}

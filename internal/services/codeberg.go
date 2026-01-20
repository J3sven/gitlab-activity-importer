package services

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/furmanp/gitlab-activity-importer/internal"
)

const codebergPageSize = 50

type codebergRepo struct {
	Name  string `json:"name"`
	Owner struct {
		Login    string `json:"login"`
		Username string `json:"username"`
	} `json:"owner"`
}

type codebergCommit struct {
	Sha     string `json:"sha"`
	ID      string `json:"id"`
	Message string `json:"message"`
	Commit  struct {
		Author struct {
			Name  string `json:"name"`
			Email string `json:"email"`
			Date  string `json:"date"`
		} `json:"author"`
	} `json:"commit"`
	Author struct {
		Name  string `json:"name"`
		Email string `json:"email"`
		Date  string `json:"date"`
	} `json:"author"`
}

func codebergBaseURL() string {
	return strings.TrimRight(os.Getenv("CODEBERG_BASE_URL"), "/")
}

func codebergAuthHeader(req *http.Request) {
	req.Header.Set("Authorization", fmt.Sprintf("token %s", os.Getenv("CODEBERG_TOKEN")))
}

func codebergAPIURL(path string) string {
	return fmt.Sprintf("%s%s", codebergBaseURL(), path)
}

func GetCodebergRepos(username string) ([]codebergRepo, error) {
	if username == "" {
		return nil, fmt.Errorf("codeberg username is not configured")
	}

	if codebergBaseURL() == "" {
		return nil, fmt.Errorf("codeberg base URL is not configured")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var allRepos []codebergRepo
	for page := 1; ; page++ {
		endpoint := codebergAPIURL(fmt.Sprintf("/api/v1/users/%s/repos?page=%d&limit=%d", username, page, codebergPageSize))
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("failed to build request: %w", err)
		}
		codebergAuthHeader(req)

		res, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making the request: %w", err)
		}

		var pageErr error
		var pageRepos []codebergRepo
		pageRepos = nil
		func() {
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(res.Body)
				pageErr = fmt.Errorf("status %d: %s", res.StatusCode, string(body))
				return
			}

			if derr := json.NewDecoder(res.Body).Decode(&pageRepos); derr != nil {
				pageErr = fmt.Errorf("decode error: %w", derr)
				return
			}

			if len(pageRepos) == 0 {
				return
			}
			allRepos = append(allRepos, pageRepos...)
		}()
		if pageErr != nil {
			return nil, pageErr
		}

		if len(pageRepos) == 0 {
			break
		}
		if len(pageRepos) < codebergPageSize {
			break
		}
	}

	return allRepos, nil
}

func GetCodebergRepoCommits(owner, repo, gitAuthor string) ([]internal.Commit, error) {
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("owner and repo must be provided")
	}

	if codebergBaseURL() == "" {
		return nil, fmt.Errorf("codeberg base URL is not configured")
	}

	client := &http.Client{Timeout: 30 * time.Second}
	var allCommits []internal.Commit
	for page := 1; ; page++ {
		endpoint := codebergAPIURL(fmt.Sprintf("/api/v1/repos/%s/%s/commits?author=%s&page=%d&limit=%d",
			url.PathEscape(owner), url.PathEscape(repo), url.QueryEscape(gitAuthor), page, codebergPageSize))
		req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("error building request: %w", err)
		}
		codebergAuthHeader(req)

		res, err := client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("error making request: %w", err)
		}

		var pageErr error
		var pageCommits []codebergCommit
		pageCommits = nil
		func() {
			defer res.Body.Close()

			if res.StatusCode != http.StatusOK {
				body, _ := io.ReadAll(res.Body)
				pageErr = fmt.Errorf("status %d: %s", res.StatusCode, string(body))
				return
			}

			if derr := json.NewDecoder(res.Body).Decode(&pageCommits); derr != nil {
				pageErr = fmt.Errorf("decode error: %w", derr)
				return
			}

			if len(pageCommits) == 0 {
				return
			}

			for _, commit := range pageCommits {
				allCommits = append(allCommits, convertCodebergCommit(commit))
			}
		}()
		if pageErr != nil {
			return nil, pageErr
		}

		if len(pageCommits) == 0 {
			break
		}
		if len(pageCommits) < codebergPageSize {
			break
		}
	}

	if len(allCommits) == 0 {
		return nil, fmt.Errorf("found no commits in repository %s/%s", owner, repo)
	}

	return allCommits, nil
}

func FetchCodebergCommits(username string, commitChannel chan []internal.Commit) {
	repos, err := GetCodebergRepos(username)
	if err != nil {
		log.Printf("Error fetching Codeberg repositories: %v", err)
		return
	}

	var wg sync.WaitGroup
	var validCommitsFound atomic.Bool
	for _, repo := range repos {
		owner := repo.Owner.Login
		if owner == "" {
			owner = repo.Owner.Username
		}
		if owner == "" || repo.Name == "" {
			continue
		}

		wg.Add(1)
		go func(owner, repoName string) {
			defer wg.Done()
			commits, err := GetCodebergRepoCommits(owner, repoName, username)
			if err != nil {
				log.Printf("Error fetching commits for %s/%s: %v", owner, repoName, err)
				return
			}
			if len(commits) > 0 {
				commitChannel <- commits
				validCommitsFound.Store(true)
			}
		}(owner, repo.Name)
	}

	wg.Wait()

	if !validCommitsFound.Load() {
		log.Println("No valid Codeberg commits found across repositories")
	}
}

func convertCodebergCommit(commit codebergCommit) internal.Commit {
	hash := commit.Sha
	if hash == "" {
		hash = commit.ID
	}

	authorName := commit.Commit.Author.Name
	if authorName == "" {
		authorName = commit.Author.Name
	}

	authorEmail := commit.Commit.Author.Email
	if authorEmail == "" {
		authorEmail = commit.Author.Email
	}

	dateStr := commit.Commit.Author.Date
	if dateStr == "" {
		dateStr = commit.Author.Date
	}

	date, err := time.Parse(time.RFC3339, dateStr)
	if err != nil {
		date = time.Now()
	}

	return internal.Commit{
		ID:           hash,
		Message:      commit.Message,
		AuthorName:   authorName,
		AuthorMail:   authorEmail,
		AuthoredDate: date,
	}
}

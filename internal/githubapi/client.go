// Package githubapi provides the small GitHub API surface needed by search.
// The production client delegates authentication and host selection to gh api.
package githubapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
)

// ErrMissingCLI identifies an environment without the GitHub CLI executable.
var ErrMissingCLI = errors.New("gh CLI is not available")

// Client is the read-only GitHub API surface used by the search command.
type Client interface {
	SearchCode(ctx context.Context, query string, page, perPage int) (CodeSearchResponse, error)
	FetchBlob(ctx context.Context, repository, sha string) ([]byte, error)
}

// CodeSearchResponse is the subset of a GitHub code-search response needed by
// the command. Unknown response fields are intentionally ignored.
type CodeSearchResponse struct {
	TotalCount int              `json:"total_count"`
	Incomplete bool             `json:"incomplete_results"`
	Items      []CodeSearchItem `json:"items"`
}

// CodeSearchItem identifies a file returned by GitHub code search.
type CodeSearchItem struct {
	Name       string               `json:"name"`
	Path       string               `json:"path"`
	SHA        string               `json:"sha"`
	Repository CodeSearchRepository `json:"repository"`
}

// CodeSearchRepository contains visibility and identity metadata embedded in
// each code-search result.
type CodeSearchRepository struct {
	FullName   string `json:"full_name"`
	Private    *bool  `json:"private"`
	Visibility string `json:"visibility"`
}

// ExecClient executes gh api through the injected command runner. It is safe to
// use in tests because no shell interpolation is involved.
type ExecClient struct {
	Runner adapter.Runner
}

// New creates a GitHub API client backed by runner.
func New(runner adapter.Runner) *ExecClient {
	return &ExecClient{Runner: runner}
}

// SearchCode performs one paginated GitHub code-search request.
func (c *ExecClient) SearchCode(ctx context.Context, query string, page, perPage int) (CodeSearchResponse, error) {
	var result CodeSearchResponse
	if c == nil || c.Runner == nil {
		return result, fmt.Errorf("%w: no command runner configured", ErrMissingCLI)
	}

	args := []string{
		"api", "-X", "GET", "search/code",
		"-f", "q=" + query,
		"-F", "per_page=" + strconv.Itoa(perPage),
		"-F", "page=" + strconv.Itoa(page),
	}
	out, stderr, err := c.Runner.Run(ctx, "gh", args...)
	if err != nil {
		return result, commandError("search/code", stderr, err)
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return result, fmt.Errorf("invalid GitHub code-search response: %w", err)
	}
	return result, nil
}

type blobResponse struct {
	Content  string `json:"content"`
	Encoding string `json:"encoding"`
}

// FetchBlob fetches and decodes a Git blob by SHA.
func (c *ExecClient) FetchBlob(ctx context.Context, repository, sha string) ([]byte, error) {
	if c == nil || c.Runner == nil {
		return nil, fmt.Errorf("%w: no command runner configured", ErrMissingCLI)
	}
	endpoint := "repos/" + repository + "/git/blobs/" + sha
	out, stderr, err := c.Runner.Run(ctx, "gh", "api", endpoint)
	if err != nil {
		return nil, commandError(endpoint, stderr, err)
	}
	var blob blobResponse
	if err := json.Unmarshal(out, &blob); err != nil {
		return nil, fmt.Errorf("invalid GitHub blob response: %w", err)
	}
	if blob.Encoding != "base64" {
		return nil, fmt.Errorf("GitHub blob response uses unsupported encoding %q", blob.Encoding)
	}
	content := strings.Join(strings.Fields(blob.Content), "")
	decoded, err := base64.StdEncoding.DecodeString(content)
	if err != nil {
		return nil, fmt.Errorf("invalid base64 content in GitHub blob response: %w", err)
	}
	return decoded, nil
}

func commandError(endpoint string, stderr []byte, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		return fmt.Errorf("%w: install GitHub CLI and ensure gh is on PATH", ErrMissingCLI)
	}
	message := strings.TrimSpace(string(stderr))
	if message == "" {
		return fmt.Errorf("GitHub API request %s failed: %w", endpoint, err)
	}
	return fmt.Errorf("GitHub API request %s failed: %s", endpoint, message)
}

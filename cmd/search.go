package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
	"github.com/f4ah6o/gh-agent-plugin/internal/githubapi"
	"github.com/f4ah6o/gh-agent-plugin/internal/output"
)

const (
	defaultSearchLimit  = 15
	maxSearchLimit      = 100
	searchPageSize      = 100
	maxSearchCandidates = 1000
)

type searchOptions struct {
	Owner string
	Limit int
	Page  int
	Query string
}

// SearchResult is one install-oriented plugin result. Marketplace entries are
// represented as individual results so their selector can be used directly.
type SearchResult struct {
	Type        string   `json:"type"`
	Name        string   `json:"name"`
	Repository  string   `json:"repository"`
	Path        string   `json:"path"`
	Agents      []string `json:"agents"`
	Description string   `json:"description"`
	Marketplace string   `json:"marketplace,omitempty"`
	Selector    string   `json:"selector,omitempty"`
}

type searchOutput struct {
	Results []SearchResult `json:"results"`
}

type manifestQuery struct {
	Filename string
	Path     string
	Agent    string
	Type     string
	Suffix   string
}

var manifestQueries = []manifestQuery{
	{Filename: "plugin.json", Path: ".claude-plugin", Agent: "claude-code", Type: "plugin", Suffix: ".claude-plugin/plugin.json"},
	{Filename: "plugin.json", Path: ".codex-plugin", Agent: "codex", Type: "plugin", Suffix: ".codex-plugin/plugin.json"},
	{Filename: "marketplace.json", Path: ".claude-plugin", Agent: "claude-code", Type: "marketplace-plugin", Suffix: ".claude-plugin/marketplace.json"},
	{Filename: "marketplace.json", Path: ".agents/plugins", Agent: "codex", Type: "marketplace-plugin", Suffix: ".agents/plugins/marketplace.json"},
}

// runSearch discovers plugin manifests without invoking a native agent or
// writing to the source cache.
func runSearch(args []string, env *Env) error {
	var cf commonFlags
	var owner string
	var limit int
	var page int
	fs := newFlagSet("search", env)
	fs.StringVar(&owner, "owner", "", "filter results to a GitHub user or organization")
	fs.IntVar(&limit, "limit", defaultSearchLimit, "maximum number of results per page")
	fs.IntVar(&limit, "L", defaultSearchLimit, "alias for --limit")
	fs.IntVar(&page, "page", 1, "logical result page to fetch")
	fs.BoolVar(&cf.jsonOut, "json", false, "emit JSON output")
	fs.StringVar(&cf.jq, "jq", "", "reserved: jq filter applied to JSON output")
	fs.StringVar(&cf.template, "template", "", "reserved: Go template applied to output")
	fs.StringVar(&cf.jsonFields, "json-fields", "", "reserved: comma-separated JSON fields")
	fs.DurationVar(&cf.timeout, "timeout", 0, "override the default operation timeout (e.g. 30s, 2m)")

	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := cf.rejectReservedFlags(fs); err != nil {
		return err
	}
	if err := requirePositionals("search", pos, 1, 1); err != nil {
		return err
	}
	query := strings.TrimSpace(pos[0])
	if query == "" {
		return exit.Errorf(exit.InvalidArguments, "search query must not be empty")
	}
	owner = strings.TrimSpace(owner)
	if owner != "" && !validGitHubOwner(owner) {
		return exit.Errorf(exit.InvalidArguments, "invalid owner %q: must be a valid GitHub username or organization", owner)
	}
	if limit < 1 || limit > maxSearchLimit {
		return exit.Errorf(exit.InvalidArguments, "invalid limit %d: must be between 1 and %d", limit, maxSearchLimit)
	}
	if page < 1 {
		return exit.Errorf(exit.InvalidArguments, "invalid page %d: must be at least 1", page)
	}

	cancel := cf.applyTimeout(env)
	defer cancel()
	if env.GitHub == nil {
		return exit.Errorf(exit.GeneralError, "GitHub search client is unavailable")
	}

	results, incomplete, err := searchPlugins(env.Ctx, env.GitHub, searchOptions{
		Owner: owner,
		Limit: limit,
		Page:  page,
		Query: query,
	})
	if err != nil {
		if errorsIsMissingCLI(err) {
			return exit.Errorf(exit.GeneralError, "%v", err)
		}
		return exit.Errorf(exit.NativeCLIFailure, "%v", err)
	}
	if incomplete {
		fmt.Fprintln(env.Stderr, "note: GitHub returned incomplete search results")
	}

	if cf.jsonOut {
		return output.JSON(env.Stdout, searchOutput{Results: results})
	}
	return output.Table(env.Stdout, searchTableHeader, searchTableRows(results))
}

// errorsIsMissingCLI keeps the command package independent of exec details.
func errorsIsMissingCLI(err error) bool {
	return errors.Is(err, githubapi.ErrMissingCLI)
}

var searchTableHeader = []string{"TYPE", "REPOSITORY", "PLUGIN", "MARKETPLACE", "AGENTS", "PATH", "DESCRIPTION"}

func searchTableRows(results []SearchResult) [][]string {
	rows := make([][]string, 0, len(results))
	for _, result := range results {
		rows = append(rows, []string{
			result.Type,
			result.Repository,
			result.Name,
			result.Marketplace,
			strings.Join(result.Agents, ","),
			result.Path,
			result.Description,
		})
	}
	return rows
}

func searchPlugins(ctx context.Context, client githubapi.Client, opts searchOptions) ([]SearchResult, bool, error) {
	need := candidateCount(opts.Page, opts.Limit)
	seenItems := make(map[string]bool)
	acc := make(map[searchKey]*SearchResult)
	var order []searchKey
	incomplete := false

	for _, query := range manifestQueries {
		items, incompleteSearch, err := searchManifest(ctx, client, query, opts, need)
		if err != nil {
			return nil, incomplete, err
		}
		incomplete = incomplete || incompleteSearch
		for _, item := range items {
			if item.Repository.FullName == "" || item.SHA == "" || seenItems[item.Repository.FullName+"\x00"+item.Path+"\x00"+item.SHA] {
				continue
			}
			seenItems[item.Repository.FullName+"\x00"+item.Path+"\x00"+item.SHA] = true
			if !isPublicRepository(item.Repository) {
				continue
			}
			parsed, ok, err := parseSearchManifest(ctx, client, query, item)
			if err != nil {
				return nil, incomplete, err
			}
			if !ok {
				continue
			}
			for _, result := range parsed {
				if !matchesSearchQuery(result, opts.Query) {
					continue
				}
				key := makeSearchKey(query, item, result)
				if current, exists := acc[key]; exists {
					mergeSearchResult(current, result)
					continue
				}
				acc[key] = &result
				order = append(order, key)
			}
		}
	}

	results := make([]SearchResult, 0, len(order))
	for _, key := range order {
		result := *acc[key]
		sort.Strings(result.Agents)
		results = append(results, result)
	}
	sort.SliceStable(results, func(i, j int) bool {
		return searchRelevance(results[i], opts.Query) > searchRelevance(results[j], opts.Query)
	})

	pageOffset := opts.Page - 1
	if pageOffset > len(results)/opts.Limit {
		return []SearchResult{}, incomplete, nil
	}
	start := pageOffset * opts.Limit
	if start >= len(results) {
		return []SearchResult{}, incomplete, nil
	}
	end := start + opts.Limit
	if end > len(results) {
		end = len(results)
	}
	return results[start:end], incomplete, nil
}

func searchManifest(ctx context.Context, client githubapi.Client, spec manifestQuery, opts searchOptions, need int) ([]githubapi.CodeSearchItem, bool, error) {
	query := fmt.Sprintf("filename:%s path:%s %s", spec.Filename, spec.Path, opts.Query)
	if opts.Owner != "" {
		query += " user:" + opts.Owner
	}
	pages := (need + searchPageSize - 1) / searchPageSize
	if pages < 1 {
		pages = 1
	}
	if pages > maxSearchCandidates/searchPageSize {
		pages = maxSearchCandidates / searchPageSize
	}
	var items []githubapi.CodeSearchItem
	incomplete := false
	for page := 1; page <= pages; page++ {
		response, err := client.SearchCode(ctx, query, page, searchPageSize)
		if err != nil {
			return nil, incomplete, err
		}
		incomplete = incomplete || response.Incomplete
		items = append(items, response.Items...)
		if len(items) >= need {
			items = items[:need]
			break
		}
		if len(response.Items) < searchPageSize {
			break
		}
	}
	return items, incomplete, nil
}

type pluginManifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type marketplaceManifest struct {
	Name        string              `json:"name"`
	Description string              `json:"description"`
	Plugins     []marketplacePlugin `json:"plugins"`
}

type marketplacePlugin struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

func parseSearchManifest(ctx context.Context, client githubapi.Client, spec manifestQuery, item githubapi.CodeSearchItem) ([]SearchResult, bool, error) {
	content, err := client.FetchBlob(ctx, item.Repository.FullName, item.SHA)
	if err != nil {
		return nil, false, err
	}
	if spec.Type == "plugin" {
		var manifest pluginManifest
		if err := json.Unmarshal(content, &manifest); err != nil || strings.TrimSpace(manifest.Name) == "" {
			return nil, false, nil
		}
		return []SearchResult{{
			Type:        spec.Type,
			Name:        manifest.Name,
			Repository:  item.Repository.FullName,
			Path:        item.Path,
			Agents:      []string{spec.Agent},
			Description: manifest.Description,
		}}, true, nil
	}

	var manifest marketplaceManifest
	if err := json.Unmarshal(content, &manifest); err != nil || strings.TrimSpace(manifest.Name) == "" {
		return nil, false, nil
	}
	results := make([]SearchResult, 0, len(manifest.Plugins))
	for _, plugin := range manifest.Plugins {
		if strings.TrimSpace(plugin.Name) == "" {
			continue
		}
		description := plugin.Description
		if description == "" {
			description = manifest.Description
		}
		results = append(results, SearchResult{
			Type:        spec.Type,
			Name:        plugin.Name,
			Repository:  item.Repository.FullName,
			Path:        item.Path,
			Agents:      []string{spec.Agent},
			Description: description,
			Marketplace: manifest.Name,
			Selector:    plugin.Name + "@" + manifest.Name,
		})
	}
	return results, len(results) > 0, nil
}

type searchKey struct {
	Type        string
	Repository  string
	LogicalPath string
	Name        string
	Marketplace string
}

func makeSearchKey(spec manifestQuery, item githubapi.CodeSearchItem, result SearchResult) searchKey {
	logicalPath := strings.TrimSuffix(item.Path, spec.Suffix)
	return searchKey{
		Type:        result.Type,
		Repository:  strings.ToLower(result.Repository),
		LogicalPath: strings.ToLower(logicalPath),
		Name:        strings.ToLower(result.Name),
		Marketplace: strings.ToLower(result.Marketplace),
	}
}

func mergeSearchResult(dst *SearchResult, src SearchResult) {
	for _, agent := range src.Agents {
		if !containsString(dst.Agents, agent) {
			dst.Agents = append(dst.Agents, agent)
		}
	}
	if src.Path < dst.Path {
		dst.Path = src.Path
	}
	if dst.Description == "" {
		dst.Description = src.Description
	}
}

func isPublicRepository(repository githubapi.CodeSearchRepository) bool {
	if repository.Private != nil {
		return !*repository.Private
	}
	return strings.EqualFold(repository.Visibility, "public")
}

func matchesSearchQuery(result SearchResult, query string) bool {
	terms := strings.Fields(normalizeSearchText(query))
	if len(terms) == 0 {
		return false
	}
	text := normalizeSearchText(strings.Join([]string{result.Name, result.Marketplace, result.Description}, " "))
	for _, term := range terms {
		if !strings.Contains(text, term) {
			return false
		}
	}
	return true
}

func searchRelevance(result SearchResult, query string) int {
	term := normalizeSearchText(query)
	name := normalizeSearchText(result.Name)
	marketplace := normalizeSearchText(result.Marketplace)
	description := normalizeSearchText(result.Description)
	score := 0
	if name == term {
		score += 3000
	} else if strings.Contains(name, term) {
		score += 1000
	}
	if strings.Contains(marketplace, term) {
		score += 500
	}
	if strings.Contains(description, term) {
		score += 100
	}
	return score
}

func normalizeSearchText(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "-", " ")
	return strings.Join(strings.Fields(value), " ")
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func candidateCount(page, limit int) int {
	base := maxSearchCandidates
	if page <= maxSearchCandidates/limit {
		base = page * limit
	}
	if base > maxSearchCandidates/3 {
		return maxSearchCandidates
	}
	return base * 3
}

func validGitHubOwner(value string) bool {
	if len(value) == 0 || len(value) > 39 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' {
			continue
		}
		return false
	}
	return true
}

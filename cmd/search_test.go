package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/f4ah6o/gh-agent-plugin/internal/adapter"
	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
	"github.com/f4ah6o/gh-agent-plugin/internal/githubapi"
)

type fakeSearchClient struct {
	searchCalls []fakeSearchCall
	blobCalls   []string
	responses   map[string]githubapi.CodeSearchResponse
	blobs       map[string][]byte
	searchErr   error
	blobErr     error
}

type fakeSearchCall struct {
	Query   string
	Page    int
	PerPage int
}

func (f *fakeSearchClient) SearchCode(_ context.Context, query string, page, perPage int) (githubapi.CodeSearchResponse, error) {
	f.searchCalls = append(f.searchCalls, fakeSearchCall{Query: query, Page: page, PerPage: perPage})
	if f.searchErr != nil {
		return githubapi.CodeSearchResponse{}, f.searchErr
	}
	if response, ok := f.responses[query]; ok {
		return response, nil
	}
	for key, response := range f.responses {
		if strings.Contains(query, key) {
			return response, nil
		}
	}
	return githubapi.CodeSearchResponse{}, nil
}

func (f *fakeSearchClient) FetchBlob(_ context.Context, repository, sha string) ([]byte, error) {
	f.blobCalls = append(f.blobCalls, repository+"/"+sha)
	if f.blobErr != nil {
		return nil, f.blobErr
	}
	return f.blobs[sha], nil
}

func TestSearch_JSONMergesDualTargetAndMarketplaceEntries(t *testing.T) {
	private := false
	client := &fakeSearchClient{
		responses: map[string]githubapi.CodeSearchResponse{
			"filename:plugin.json path:.claude-plugin": {
				Items: []githubapi.CodeSearchItem{{
					Path: "plugins/example/.claude-plugin/plugin.json", SHA: "claude-plugin",
					Repository: githubapi.CodeSearchRepository{FullName: "acme/plugins", Private: &private},
				}},
			},
			"filename:plugin.json path:.codex-plugin": {
				Items: []githubapi.CodeSearchItem{{
					Path: "plugins/example/.codex-plugin/plugin.json", SHA: "codex-plugin",
					Repository: githubapi.CodeSearchRepository{FullName: "acme/plugins", Private: &private},
				}},
			},
			"filename:marketplace.json path:.claude-plugin": {
				Items: []githubapi.CodeSearchItem{{
					Path: ".claude-plugin/marketplace.json", SHA: "claude-marketplace",
					Repository: githubapi.CodeSearchRepository{FullName: "acme/plugins", Private: &private},
				}},
			},
			"filename:marketplace.json path:.agents/plugins": {
				Items: []githubapi.CodeSearchItem{{
					Path: ".agents/plugins/marketplace.json", SHA: "codex-marketplace",
					Repository: githubapi.CodeSearchRepository{FullName: "acme/plugins", Private: &private},
				}},
			},
		},
		blobs: map[string][]byte{
			"claude-plugin":      []byte(`{"name":"example","description":"Example plugin"}`),
			"codex-plugin":       []byte(`{"name":"example","description":"Example plugin"}`),
			"claude-marketplace": []byte(`{"name":"sample","plugins":[{"name":"example","description":"Example catalog entry"}]}`),
			"codex-marketplace":  []byte(`{"name":"sample","plugins":[{"name":"example","description":"Example catalog entry"}]}`),
		},
	}
	runner := &adapter.RecordingRunner{}
	env, out, errOut := newTestEnv(runner)
	env.GitHub = client

	if code := Execute([]string{"search", "example", "--json", "--limit", "10"}, env); code != exit.OK {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, errOut.String())
	}
	var got searchOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out.String())
	}
	if len(got.Results) != 2 {
		t.Fatalf("results = %+v, want direct and marketplace entries", got.Results)
	}

	var direct, marketplace SearchResult
	for _, result := range got.Results {
		switch result.Type {
		case "plugin":
			direct = result
		case "marketplace-plugin":
			marketplace = result
		default:
			t.Fatalf("unexpected result type %q", result.Type)
		}
	}
	if !sameStrings(direct.Agents, []string{"claude-code", "codex"}) {
		t.Fatalf("direct agents = %v", direct.Agents)
	}
	if direct.Path != "plugins/example/.claude-plugin/plugin.json" {
		t.Fatalf("direct path = %q", direct.Path)
	}
	if marketplace.Selector != "example@sample" || marketplace.Marketplace != "sample" {
		t.Fatalf("marketplace result = %+v", marketplace)
	}
	if !sameStrings(marketplace.Agents, []string{"claude-code", "codex"}) {
		t.Fatalf("marketplace agents = %v", marketplace.Agents)
	}
	if len(runner.Calls) != 0 {
		t.Fatalf("search must not call native agents: %v", runner.Calls)
	}
	if len(client.blobCalls) != 4 {
		t.Fatalf("blob calls = %d, want 4", len(client.blobCalls))
	}
}

func TestSearch_EmptyJSONIsSuccessful(t *testing.T) {
	client := &fakeSearchClient{responses: map[string]githubapi.CodeSearchResponse{}}
	env, out, errOut := newTestEnv(&adapter.RecordingRunner{})
	env.GitHub = client

	if code := Execute([]string{"search", "missing", "--json"}, env); code != exit.OK {
		t.Fatalf("exit = %d, want 0; stderr: %s", code, errOut.String())
	}
	var got searchOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if got.Results == nil || len(got.Results) != 0 {
		t.Fatalf("results = %#v, want non-nil empty array", got.Results)
	}
}

func TestSearch_RejectsInvalidArgumentsBeforeClient(t *testing.T) {
	cases := [][]string{
		{"search"},
		{"search", "one", "two"},
		{"search", "one", "--limit", "0"},
		{"search", "one", "--limit", "101"},
		{"search", "one", "--page", "0"},
		{"search", "one", "--owner", "bad_owner"},
		{"search", "one", "--jq", ".results"},
	}
	for _, args := range cases {
		t.Run(strings.Join(args, "_"), func(t *testing.T) {
			client := &fakeSearchClient{}
			env, _, _ := newTestEnv(&adapter.RecordingRunner{})
			env.GitHub = client
			if code := Execute(args, env); code != exit.InvalidArguments {
				t.Fatalf("exit = %d, want %d", code, exit.InvalidArguments)
			}
			if len(client.searchCalls) != 0 {
				t.Fatalf("client calls = %v, want none", client.searchCalls)
			}
		})
	}
}

func TestSearch_PropagatesOwnerAndPagination(t *testing.T) {
	client := &fakeSearchClient{}
	env, _, _ := newTestEnv(&adapter.RecordingRunner{})
	env.GitHub = client

	if code := Execute([]string{"search", "terraform", "--owner", "acme", "--limit", "5", "--page", "2"}, env); code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	if len(client.searchCalls) != len(manifestQueries) {
		t.Fatalf("search calls = %d, want %d", len(client.searchCalls), len(manifestQueries))
	}
	for _, call := range client.searchCalls {
		if !strings.Contains(call.Query, "user:acme") {
			t.Errorf("query %q does not contain owner filter", call.Query)
		}
		if call.Page != 1 || call.PerPage != searchPageSize {
			t.Errorf("call pagination = page %d/per_page %d", call.Page, call.PerPage)
		}
	}
}

func TestSearch_FiltersPrivateRepositories(t *testing.T) {
	private := true
	client := &fakeSearchClient{
		responses: map[string]githubapi.CodeSearchResponse{
			"filename:plugin.json path:.claude-plugin": {
				Items: []githubapi.CodeSearchItem{{
					Path: "plugin/.claude-plugin/plugin.json", SHA: "private",
					Repository: githubapi.CodeSearchRepository{FullName: "acme/private", Private: &private},
				}},
			},
		},
		blobs: map[string][]byte{"private": []byte(`{"name":"secret","description":"secret"}`)},
	}
	env, out, _ := newTestEnv(&adapter.RecordingRunner{})
	env.GitHub = client

	if code := Execute([]string{"search", "secret", "--json"}, env); code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	var got searchOutput
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if len(got.Results) != 0 || len(client.blobCalls) != 0 {
		t.Fatalf("private result = %#v, blob calls = %v", got.Results, client.blobCalls)
	}
}

func TestSearch_TableOutputIsStable(t *testing.T) {
	private := false
	client := &fakeSearchClient{
		responses: map[string]githubapi.CodeSearchResponse{
			"filename:plugin.json path:.claude-plugin": {
				Items: []githubapi.CodeSearchItem{{
					Path: "plugins/example/.claude-plugin/plugin.json", SHA: "example",
					Repository: githubapi.CodeSearchRepository{FullName: "acme/plugins", Private: &private},
				}},
			},
		},
		blobs: map[string][]byte{"example": []byte(`{"name":"example","description":"Example plugin"}`)},
	}
	env, out, _ := newTestEnv(&adapter.RecordingRunner{})
	env.GitHub = client

	if code := Execute([]string{"search", "example"}, env); code != exit.OK {
		t.Fatalf("exit = %d, want 0", code)
	}
	want := "TYPE    REPOSITORY    PLUGIN   MARKETPLACE  AGENTS       PATH                                        DESCRIPTION\n" +
		"plugin  acme/plugins  example  -            claude-code  plugins/example/.claude-plugin/plugin.json  Example plugin\n"
	if out.String() != want {
		t.Fatalf("table output = %q, want %q", out.String(), want)
	}
}

func TestSearch_APIErrorReturnsNativeFailure(t *testing.T) {
	client := &fakeSearchClient{searchErr: errors.New("rate limit exceeded")}
	env, _, _ := newTestEnv(&adapter.RecordingRunner{})
	env.GitHub = client

	if code := Execute([]string{"search", "terraform"}, env); code != exit.NativeCLIFailure {
		t.Fatalf("exit = %d, want %d", code, exit.NativeCLIFailure)
	}
}

func TestSearch_BlobErrorReturnsNativeFailure(t *testing.T) {
	private := false
	client := &fakeSearchClient{
		responses: map[string]githubapi.CodeSearchResponse{
			"filename:plugin.json path:.claude-plugin": {
				Items: []githubapi.CodeSearchItem{{
					Path: "plugin/.claude-plugin/plugin.json", SHA: "broken",
					Repository: githubapi.CodeSearchRepository{FullName: "acme/plugins", Private: &private},
				}},
			},
		},
		blobErr: errors.New("blob unavailable"),
	}
	env, _, _ := newTestEnv(&adapter.RecordingRunner{})
	env.GitHub = client

	if code := Execute([]string{"search", "plugin"}, env); code != exit.NativeCLIFailure {
		t.Fatalf("exit = %d, want %d", code, exit.NativeCLIFailure)
	}
}

func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

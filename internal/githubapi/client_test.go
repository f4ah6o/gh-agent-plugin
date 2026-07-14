package githubapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os/exec"
	"strings"
	"testing"
)

type fakeRunner struct {
	calls    []fakeCall
	stdout   map[string][]byte
	stderr   map[string][]byte
	errors   map[string]error
	defaultE error
}

type fakeCall struct {
	Name string
	Args []string
}

func (f *fakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, error) {
	call := fakeCall{Name: name, Args: append([]string(nil), args...)}
	f.calls = append(f.calls, call)
	key := name + " " + strings.Join(args, " ")
	return f.stdout[key], f.stderr[key], errorFor(f.errors, key, f.defaultE)
}

func (f *fakeRunner) Look(string) (string, error) { return "/usr/bin/gh", nil }

func errorFor(values map[string]error, key string, fallback error) error {
	if err, ok := values[key]; ok {
		return err
	}
	return fallback
}

func TestExecClient_SearchCode(t *testing.T) {
	runner := &fakeRunner{stdout: map[string][]byte{}}
	key := "gh api -X GET search/code -f q=filename:plugin.json path:.claude-plugin terraform -F per_page=100 -F page=2"
	runner.stdout[key] = []byte(`{"total_count":1,"incomplete_results":false,"items":[{"name":"plugin.json","path":".claude-plugin/plugin.json","sha":"abc","repository":{"full_name":"acme/plugins","private":false}}]}`)
	client := New(runner)

	got, err := client.SearchCode(context.Background(), "filename:plugin.json path:.claude-plugin terraform", 2, 100)
	if err != nil {
		t.Fatalf("SearchCode: %v", err)
	}
	if got.TotalCount != 1 || len(got.Items) != 1 || got.Items[0].Repository.FullName != "acme/plugins" {
		t.Fatalf("unexpected response: %+v", got)
	}
	if len(runner.calls) != 1 || runner.calls[0].Name != "gh" {
		t.Fatalf("calls = %+v", runner.calls)
	}
}

func TestExecClient_FetchBlobDecodesBase64(t *testing.T) {
	runner := &fakeRunner{stdout: map[string][]byte{}}
	content := []byte(`{"name":"example"}`)
	encoded := base64.StdEncoding.EncodeToString(content)
	key := "gh api repos/acme/plugins/git/blobs/abc"
	runner.stdout[key] = mustJSON(t, map[string]string{"encoding": "base64", "content": encoded})
	client := New(runner)

	got, err := client.FetchBlob(context.Background(), "acme/plugins", "abc")
	if err != nil {
		t.Fatalf("FetchBlob: %v", err)
	}
	if string(got) != string(content) {
		t.Fatalf("content = %q, want %q", got, content)
	}
}

func TestExecClient_MissingCLI(t *testing.T) {
	runner := &fakeRunner{errors: map[string]error{}}
	runner.defaultE = exec.ErrNotFound
	client := New(runner)

	_, err := client.SearchCode(context.Background(), "terraform", 1, 100)
	if !errors.Is(err, ErrMissingCLI) {
		t.Fatalf("error = %v, want ErrMissingCLI", err)
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	out, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

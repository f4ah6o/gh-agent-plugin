package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
	"github.com/f4ah6o/gh-agent-plugin/internal/output"
)

// runIssue dispatches issue sub-subcommands.
func runIssue(args []string, env *Env) error {
	if len(args) == 0 {
		return exit.Errorf(exit.InvalidArguments, "issue requires a subcommand: list|view|comment")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "list", "ls":
		return issueList(rest, env)
	case "view":
		return issueView(rest, env)
	case "comment":
		return issueComment(rest, env)
	default:
		return exit.Errorf(exit.InvalidArguments, "unknown issue subcommand %q", sub)
	}
}

func issueList(args []string, env *Env) error {
	var cf commonFlags
	var state, label, repo string
	var limit int
	fs := newFlagSet("issue list", env)
	cf.register(fs)
	fs.StringVar(&state, "state", "open", "filter by state: open|closed|all")
	fs.StringVar(&label, "label", "", "filter by label")
	fs.StringVar(&repo, "repo", "", "target repository (OWNER/REPO)")
	fs.IntVar(&limit, "limit", 30, "maximum number of issues to list")
	if _, err := parseArgs(fs, args); err != nil {
		return err
	}
	if err := cf.rejectReservedFlags(fs); err != nil {
		return err
	}
	cancel := cf.applyTimeout(env)
	defer cancel()

	ghArgs := []string{"issue", "list",
		"--state", state,
		"--limit", fmt.Sprint(limit),
		"--json", "number,title,state,labels,author,createdAt,url",
	}
	if repo != "" {
		ghArgs = append(ghArgs, "--repo", repo)
	}
	if label != "" {
		ghArgs = append(ghArgs, "--label", label)
	}

	raw, err := runGH(env, ghArgs...)
	if err != nil {
		return err
	}

	if cf.jsonOut {
		_, err = env.Stdout.Write(raw)
		return err
	}

	var issues []struct {
		Number    int                     `json:"number"`
		Title     string                  `json:"title"`
		State     string                  `json:"state"`
		Author    struct{ Login string }  `json:"author"`
		CreatedAt string                  `json:"createdAt"`
		Labels    []struct{ Name string } `json:"labels"`
	}
	if err := json.Unmarshal(raw, &issues); err != nil {
		return exit.Errorf(exit.NativeCLIFailure, "failed to parse gh issue list output: %v", err)
	}

	header := []string{"NUMBER", "TITLE", "STATE", "AUTHOR", "LABELS"}
	rows := make([][]string, 0, len(issues))
	for _, iss := range issues {
		lbls := make([]string, 0, len(iss.Labels))
		for _, l := range iss.Labels {
			lbls = append(lbls, l.Name)
		}
		rows = append(rows, []string{
			fmt.Sprint(iss.Number),
			iss.Title,
			iss.State,
			iss.Author.Login,
			strings.Join(lbls, ","),
		})
	}
	return output.Table(env.Stdout, header, rows)
}

func issueView(args []string, env *Env) error {
	var cf commonFlags
	var repo string
	var withComments bool
	fs := newFlagSet("issue view", env)
	cf.register(fs)
	fs.StringVar(&repo, "repo", "", "target repository (OWNER/REPO)")
	fs.BoolVar(&withComments, "comments", false, "include comments in output")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := cf.rejectReservedFlags(fs); err != nil {
		return err
	}
	cancel := cf.applyTimeout(env)
	defer cancel()

	if len(pos) == 0 {
		return exit.Errorf(exit.InvalidArguments, "usage: issue view NUMBER")
	}

	fields := "number,title,state,body,author,labels,url,createdAt"
	if withComments {
		fields += ",comments"
	}
	ghArgs := []string{"issue", "view", pos[0], "--json", fields}
	if repo != "" {
		ghArgs = append(ghArgs, "--repo", repo)
	}

	raw, err := runGH(env, ghArgs...)
	if err != nil {
		return err
	}

	if cf.jsonOut {
		_, err = env.Stdout.Write(raw)
		return err
	}

	var iss struct {
		Number    int                     `json:"number"`
		Title     string                  `json:"title"`
		State     string                  `json:"state"`
		Body      string                  `json:"body"`
		Author    struct{ Login string }  `json:"author"`
		URL       string                  `json:"url"`
		CreatedAt string                  `json:"createdAt"`
		Labels    []struct{ Name string } `json:"labels"`
		Comments  []struct {
			Author struct{ Login string } `json:"author"`
			Body   string                 `json:"body"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(raw, &iss); err != nil {
		return exit.Errorf(exit.NativeCLIFailure, "failed to parse gh issue view output: %v", err)
	}

	lbls := make([]string, 0, len(iss.Labels))
	for _, l := range iss.Labels {
		lbls = append(lbls, l.Name)
	}

	w := env.Stdout
	fmt.Fprintf(w, "#%d %s [%s]\n", iss.Number, iss.Title, iss.State)
	fmt.Fprintf(w, "Author: %s  Created: %s\n", iss.Author.Login, iss.CreatedAt)
	if len(lbls) > 0 {
		fmt.Fprintf(w, "Labels: %s\n", strings.Join(lbls, ", "))
	}
	fmt.Fprintf(w, "URL:    %s\n\n", iss.URL)
	fmt.Fprintln(w, iss.Body)
	for _, c := range iss.Comments {
		fmt.Fprintf(w, "\n--- %s ---\n%s\n", c.Author.Login, c.Body)
	}
	return nil
}

func issueComment(args []string, env *Env) error {
	var cf commonFlags
	var body, repo string
	fs := newFlagSet("issue comment", env)
	cf.register(fs)
	fs.StringVar(&body, "body", "", "comment body (required)")
	fs.StringVar(&repo, "repo", "", "target repository (OWNER/REPO)")
	pos, err := parseArgs(fs, args)
	if err != nil {
		return err
	}
	if err := cf.rejectReservedFlags(fs); err != nil {
		return err
	}
	cancel := cf.applyTimeout(env)
	defer cancel()

	if len(pos) == 0 {
		return exit.Errorf(exit.InvalidArguments, "usage: issue comment NUMBER --body TEXT")
	}
	if body == "" {
		return exit.Errorf(exit.InvalidArguments, "--body is required")
	}

	if cf.dryRun {
		dryArgs := append([]string{"issue", "comment", pos[0], "--body", body}, repoFlag(repo)...)
		fmt.Fprintf(env.Stdout, "dry-run: gh %s\n", strings.Join(dryArgs, " "))
		return nil
	}

	ghArgs := []string{"issue", "comment", pos[0], "--body", body}
	if repo != "" {
		ghArgs = append(ghArgs, "--repo", repo)
	}
	if _, err := runGH(env, ghArgs...); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "comment added to issue #%s\n", pos[0])
	return nil
}

// runGH finds the gh binary and runs it with the given subcommand arguments.
func runGH(env *Env, args ...string) ([]byte, error) {
	if env.Runner == nil {
		return nil, exit.Errorf(exit.AgentNotInstalled, "gh CLI not available (no runner configured)")
	}
	ghPath, err := env.Runner.Look("gh")
	if err != nil {
		return nil, exit.Errorf(exit.AgentNotInstalled, "gh CLI not found in PATH")
	}
	stdout, stderr, err := env.Runner.Run(env.Ctx, ghPath, args...)
	if err != nil {
		msg := strings.TrimSpace(string(stderr))
		if msg == "" {
			msg = err.Error()
		}
		return nil, exit.Errorf(exit.NativeCLIFailure, "gh %s: %s", args[0], msg)
	}
	return stdout, nil
}

// repoFlag returns ["--repo", repo] when repo is non-empty, else nil.
func repoFlag(repo string) []string {
	if repo == "" {
		return nil
	}
	return []string{"--repo", repo}
}

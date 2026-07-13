package cmd

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
	"github.com/f4ah6o/gh-agent-plugin/internal/output"
)

// runPR dispatches pr sub-subcommands.
func runPR(args []string, env *Env) error {
	if len(args) == 0 {
		return exit.Errorf(exit.InvalidArguments, "pr requires a subcommand: comment")
	}
	sub := args[0]
	rest := args[1:]
	switch sub {
	case "comment":
		return runPRComment(rest, env)
	default:
		return exit.Errorf(exit.InvalidArguments, "unknown pr subcommand %q", sub)
	}
}

// runPRComment dispatches pr comment sub-subcommands: add or list.
func runPRComment(args []string, env *Env) error {
	if len(args) == 0 {
		return exit.Errorf(exit.InvalidArguments, "pr comment requires a subcommand or PR number: add NUMBER|list NUMBER")
	}
	// If the first arg is "list", dispatch to prCommentList.
	// Otherwise treat first arg as a PR number for the add path.
	if args[0] == "list" {
		return prCommentList(args[1:], env)
	}
	return prCommentAdd(args, env)
}

// prCommentAdd posts a comment on a pull request.
// Usage: pr comment NUMBER --body TEXT [--repo OWNER/REPO]
func prCommentAdd(args []string, env *Env) error {
	var cf commonFlags
	var body, repo string
	fs := newFlagSet("pr comment", env)
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
		return exit.Errorf(exit.InvalidArguments, "usage: pr comment NUMBER --body TEXT")
	}
	if body == "" {
		return exit.Errorf(exit.InvalidArguments, "--body is required")
	}

	if cf.dryRun {
		dryArgs := append([]string{"pr", "comment", pos[0], "--body", body}, repoFlag(repo)...)
		fmt.Fprintf(env.Stdout, "dry-run: gh %s\n", strings.Join(dryArgs, " "))
		return nil
	}

	ghArgs := []string{"pr", "comment", pos[0], "--body", body}
	if repo != "" {
		ghArgs = append(ghArgs, "--repo", repo)
	}
	if _, err := runGH(env, ghArgs...); err != nil {
		return err
	}
	fmt.Fprintf(env.Stdout, "comment added to PR #%s\n", pos[0])
	return nil
}

// prCommentList lists comments on a pull request.
// Usage: pr comment list NUMBER [--repo OWNER/REPO] [--json]
func prCommentList(args []string, env *Env) error {
	var cf commonFlags
	var repo string
	fs := newFlagSet("pr comment list", env)
	cf.register(fs)
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
		return exit.Errorf(exit.InvalidArguments, "usage: pr comment list NUMBER")
	}

	ghArgs := []string{"pr", "view", pos[0], "--json", "comments"}
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

	var pr struct {
		Comments []struct {
			Author struct{ Login string } `json:"author"`
			Body   string                 `json:"body"`
			URL    string                 `json:"url"`
		} `json:"comments"`
	}
	if err := json.Unmarshal(raw, &pr); err != nil {
		return exit.Errorf(exit.NativeCLIFailure, "failed to parse pr view output: %v", err)
	}

	if len(pr.Comments) == 0 {
		fmt.Fprintln(env.Stdout, "no comments")
		return nil
	}

	header := []string{"AUTHOR", "BODY"}
	rows := make([][]string, 0, len(pr.Comments))
	for _, c := range pr.Comments {
		rows = append(rows, []string{c.Author.Login, c.Body})
	}
	return output.Table(env.Stdout, header, rows)
}

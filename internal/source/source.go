// Package source parses the plugin source and selector syntax accepted by the
// install/preview/remove commands (issue #1, "Source and selector syntax").
//
// Supported forms:
//
//	OWNER/REPO PLUGIN          --> GitHub repository source
//	PLUGIN@MARKETPLACE         --> already-configured marketplace selector
//	./path/to/repo PLUGIN      --> local source (with --from-local)
//
// A git ref is supplied via the --ref flag, never via the selector "@", which is
// reserved for PLUGIN@MARKETPLACE.
package source

import (
	"strings"

	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
)

// Kind enumerates the parsed source kinds.
type Kind int

const (
	KindGitHub Kind = iota
	KindLocal
	KindMarketplace
)

func (k Kind) String() string {
	switch k {
	case KindGitHub:
		return "github"
	case KindLocal:
		return "local"
	case KindMarketplace:
		return "marketplace"
	default:
		return "unknown"
	}
}

// Spec is a parsed source + selector.
type Spec struct {
	Kind        Kind
	Repository  string // OWNER/REPO for github
	Path        string // local filesystem path for local
	Plugin      string // plugin name
	Marketplace string // marketplace name for marketplace selector
	Ref         string // git ref from --ref
}

// Parse interprets positional args plus the --ref and --from-local flags.
//
// args contains the non-flag positional arguments for the command.
func Parse(args []string, ref string, fromLocal bool) (Spec, error) {
	if len(args) == 0 {
		return Spec{}, exit.Errorf(exit.InvalidArguments, "a plugin source or selector is required")
	}

	if fromLocal {
		if len(args) < 2 {
			return Spec{}, exit.Errorf(exit.InvalidArguments, "local source requires PATH and PLUGIN")
		}
		return Spec{Kind: KindLocal, Path: args[0], Plugin: args[1], Ref: ref}, nil
	}

	first := args[0]

	// PLUGIN@MARKETPLACE selector (single argument containing '@', no slash).
	if len(args) == 1 && strings.Contains(first, "@") && !strings.Contains(first, "/") {
		name, market, ok := splitSelector(first)
		if !ok {
			return Spec{}, exit.Errorf(exit.InvalidArguments, "invalid selector %q, expected PLUGIN@MARKETPLACE", first)
		}
		return Spec{Kind: KindMarketplace, Plugin: name, Marketplace: market, Ref: ref}, nil
	}

	// OWNER/REPO PLUGIN github source.
	if strings.Contains(first, "/") {
		if len(args) < 2 {
			return Spec{}, exit.Errorf(exit.InvalidArguments, "github source requires OWNER/REPO and PLUGIN")
		}
		if !validRepo(first) {
			return Spec{}, exit.Errorf(exit.InvalidArguments, "invalid repository %q, expected OWNER/REPO", first)
		}
		return Spec{Kind: KindGitHub, Repository: first, Plugin: args[1], Ref: ref}, nil
	}

	return Spec{}, exit.Errorf(exit.InvalidArguments, "could not interpret source %q; use OWNER/REPO PLUGIN, PLUGIN@MARKETPLACE, or --from-local", first)
}

// splitSelector splits PLUGIN@MARKETPLACE.
func splitSelector(s string) (plugin, marketplace string, ok bool) {
	i := strings.IndexByte(s, '@')
	if i <= 0 || i == len(s)-1 {
		return "", "", false
	}
	return s[:i], s[i+1:], true
}

// validRepo checks the OWNER/REPO shape (exactly one slash, both parts non-empty).
func validRepo(s string) bool {
	parts := strings.Split(s, "/")
	if len(parts) != 2 {
		return false
	}
	return parts[0] != "" && parts[1] != ""
}

package source

import (
	"testing"

	"github.com/f4ah6o/gh-agent-plugin/internal/exit"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name      string
		args      []string
		ref       string
		fromLocal bool
		want      Spec
		wantErr   bool
	}{
		{
			name: "github source",
			args: []string{"acme/plugins", "formatter"},
			ref:  "v1.2.0",
			want: Spec{Kind: KindGitHub, Repository: "acme/plugins", Plugin: "formatter", Ref: "v1.2.0"},
		},
		{
			name: "marketplace selector",
			args: []string{"formatter@company"},
			want: Spec{Kind: KindMarketplace, Plugin: "formatter", Marketplace: "company"},
		},
		{
			name:      "local source",
			args:      []string{"./repo", "formatter"},
			fromLocal: true,
			want:      Spec{Kind: KindLocal, Path: "./repo", Plugin: "formatter"},
		},
		{name: "empty", args: nil, wantErr: true},
		{name: "github missing plugin", args: []string{"acme/plugins"}, wantErr: true},
		{name: "github extra positional", args: []string{"acme/plugins", "formatter", "extra"}, wantErr: true},
		{name: "bad selector", args: []string{"@company"}, wantErr: true},
		{name: "marketplace selector extra positional", args: []string{"formatter@company", "extra"}, wantErr: true},
		{name: "local missing plugin", args: []string{"./repo"}, fromLocal: true, wantErr: true},
		{name: "local extra positional", args: []string{"./repo", "formatter", "extra"}, fromLocal: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.args, tt.ref, tt.fromLocal)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
				}
				if code := exit.CodeOf(err); code != exit.InvalidArguments {
					t.Fatalf("error code = %d, want %d", code, exit.InvalidArguments)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

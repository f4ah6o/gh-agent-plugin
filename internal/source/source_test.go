package source

import "testing"

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
		{name: "bad selector", args: []string{"@company"}, wantErr: true},
		{name: "local missing plugin", args: []string{"./repo"}, fromLocal: true, wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.args, tt.ref, tt.fromLocal)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got %+v", got)
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

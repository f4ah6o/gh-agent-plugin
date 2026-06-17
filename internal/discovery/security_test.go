package discovery

import "testing"

func TestScan_SampleFindings(t *testing.T) {
	dp, err := DiscoverPlugin(sampleRepo(t), "example")
	if err != nil {
		t.Fatalf("discover: %v", err)
	}
	findings := Scan(dp)

	rules := map[string]bool{}
	for _, f := range findings {
		rules[f.Rule] = true
	}

	for _, want := range []string{
		"shell-hook",           // hooks/pre-commit.sh
		"mcp-external-process", // local-tool launches node
		"insecure-url",         // remote-tool http:// URL
		"credential-env",       // EXAMPLE_API_KEY
	} {
		if !rules[want] {
			t.Errorf("expected finding %q, got rules %v", want, rules)
		}
	}
}

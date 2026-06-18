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

	// Every finding must carry one of the three defined severities, and the
	// http:// URL must be graded blocking.
	bySeverity := map[Severity]int{}
	for _, f := range findings {
		switch f.Severity {
		case SeverityInfo, SeverityWarning, SeverityBlocking:
			bySeverity[f.Severity]++
		default:
			t.Errorf("finding %q has unexpected severity %q", f.Rule, f.Severity)
		}
		if f.Rule == "insecure-url" && f.Severity != SeverityBlocking {
			t.Errorf("insecure-url severity = %q, want blocking", f.Severity)
		}
	}
	if bySeverity[SeverityBlocking] == 0 {
		t.Errorf("expected at least one blocking finding, got %v", bySeverity)
	}

	// Findings are ordered most-severe first.
	for i := 1; i < len(findings); i++ {
		if severityRank(findings[i-1].Severity) > severityRank(findings[i].Severity) {
			t.Errorf("findings not sorted by severity: %v", findings)
			break
		}
	}
}

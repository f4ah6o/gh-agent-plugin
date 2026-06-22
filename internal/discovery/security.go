package discovery

import "github.com/f4ah6o/gh-agent-plugin/internal/security"

type Severity = security.Severity
type Finding = security.Finding

const (
	SeverityInfo     = security.SeverityInfo
	SeverityWarning  = security.SeverityWarning
	SeverityBlocking = security.SeverityBlocking
)

// Scan preserves the original discovery API while routing baseline checks
// through the first-party security scanner.
func Scan(dp DiscoveredPlugin) []Finding {
	report, err := security.Scan(dp.Root, security.Options{})
	if err != nil {
		return nil
	}
	return report.Findings
}

func severityRank(s Severity) int {
	switch s {
	case SeverityBlocking:
		return 0
	case SeverityWarning:
		return 1
	default:
		return 2
	}
}

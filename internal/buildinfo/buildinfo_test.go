package buildinfo

import (
	"strings"
	"testing"
)

func TestSummaryIncludesBuildIdentity(t *testing.T) {
	originalVersion, originalCommit, originalDate := Version, Commit, SourceDate
	t.Cleanup(func() {
		Version, Commit, SourceDate = originalVersion, originalCommit, originalDate
	})

	Version, Commit, SourceDate = "1.2.3", "abc123", "2026-07-10T00:00:00Z"
	summary := Summary()
	for _, expected := range []string{"TwinTidy 1.2.3", "abc123", "2026-07-10T00:00:00Z"} {
		if !strings.Contains(summary, expected) {
			t.Fatalf("Summary() = %q, want it to contain %q", summary, expected)
		}
	}
}

package buildinfo

import "fmt"

var (
	Version    = "dev"
	Commit     = "unknown"
	SourceDate = "unknown"
)

func Summary() string {
	return fmt.Sprintf("TwinTidy %s (commit %s, source date %s)", Version, Commit, SourceDate)
}

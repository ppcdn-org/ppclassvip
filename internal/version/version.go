package version

import "fmt"

var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

func String() string {
	return fmt.Sprintf("%s (commit=%s, built=%s)", Version, Commit, BuildTime)
}

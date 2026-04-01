package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShimBuildInfo(t *testing.T) {
	version, gitCommit := shimBuildInfo()

	require.NotEmpty(t, version, "version must not be empty")

	// When run from a git checkout via `go test`, vcs.revision is stamped.
	// Verify it looks like a hex SHA when present.
	if gitCommit != "" {
		require.Regexp(t, `^[0-9a-f]+$`, gitCommit, "GitCommit must be a hex SHA")
	}
}

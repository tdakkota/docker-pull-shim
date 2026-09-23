package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildInfo(t *testing.T) {
	info := buildInfo()
	require.NotEmpty(t, info.Version)
	if info.Commit != "" {
		require.Regexp(t, `^[0-9a-f]+$`, info.Commit)
	}
}

package util

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExpandTilde_WithTilde(t *testing.T) {
	home, err := os.UserHomeDir()
	require.NoError(t, err)

	result := expandTilde("~/.kube/config")
	assert.Equal(t, filepath.Join(home, ".kube", "config"), result)
}

func TestExpandTilde_WithoutTilde(t *testing.T) {
	result := expandTilde("/etc/kubernetes/config")
	assert.Equal(t, "/etc/kubernetes/config", result)
}

func TestExpandTilde_Empty(t *testing.T) {
	result := expandTilde("")
	assert.Equal(t, "", result)
}

func TestExpandTilde_TildeOnly(t *testing.T) {
	result := expandTilde("~")
	assert.Equal(t, "~", result)
}

func TestExpandTilde_TildeInMiddle(t *testing.T) {
	result := expandTilde("/home/user/~/config")
	assert.Equal(t, "/home/user/~/config", result)
}

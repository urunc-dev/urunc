package unikontainers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveAgainstBase(t *testing.T) {
	t.Run("absolute path is returned as is", func(t *testing.T) {
		result, err := resolveAgainstBase("/some/base", "/absolute/path")
		assert.NoError(t, err)
		assert.Equal(t, "/absolute/path", result)
	})

	t.Run("relative path is joined with absolute base", func(t *testing.T) {
		result, err := resolveAgainstBase("/home/shreya", "documents/file.txt")
		assert.NoError(t, err)
		assert.Equal(t, "/home/shreya/documents/file.txt", result)
	})

	t.Run("relative path with relative base", func(t *testing.T) {
		result, err := resolveAgainstBase("mybase", "myfile.txt")
		assert.NoError(t, err)
		assert.Contains(t, result, "mybase/myfile.txt")
	})
}

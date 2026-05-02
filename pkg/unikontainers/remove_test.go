package unikontainers

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for remove() - removes element from a string slice by index
func TestRemove(t *testing.T) {
	t.Run("remove first element", func(t *testing.T) {
		s := []string{"a", "b", "c"}
		result := remove(s, 0)
		assert.Len(t, result, 2)
	})

	t.Run("remove middle element", func(t *testing.T) {
		s := []string{"a", "b", "c"}
		result := remove(s, 1)
		assert.Len(t, result, 2)
	})

	t.Run("remove only element", func(t *testing.T) {
		s := []string{"a"}
		result := remove(s, 0)
		assert.Len(t, result, 0)
	})
}

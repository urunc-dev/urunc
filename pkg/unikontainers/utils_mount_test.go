package unikontainers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// Tests for convertUint32ToIntSlice() - converts []uint32 to []int
func TestConvertUint32ToIntSlice(t *testing.T) {
	t.Run("normal conversion", func(t *testing.T) {
		input := []uint32{1, 2, 3}
		result := convertUint32ToIntSlice(input, 3)
		assert.Equal(t, []int{1, 2, 3}, result)
	})

	t.Run("empty slice", func(t *testing.T) {
		input := []uint32{}
		result := convertUint32ToIntSlice(input, 0)
		assert.Equal(t, []int{}, result)
	})

	t.Run("single element", func(t *testing.T) {
		input := []uint32{42}
		result := convertUint32ToIntSlice(input, 1)
		assert.Equal(t, []int{42}, result)
	})
}

// Tests for rmMultipleDirs() - removes multiple directories under a prefix path
func TestRmMultipleDirs(t *testing.T) {
	t.Run("remove existing dirs", func(t *testing.T) {
		tmpDir := t.TempDir()
		dirs := []string{"dir1", "dir2"}
		for _, d := range dirs {
			err := os.MkdirAll(filepath.Join(tmpDir, d), 0755)
			assert.NoError(t, err)
		}
		err := rmMultipleDirs(tmpDir, dirs)
		assert.NoError(t, err)
		for _, d := range dirs {
			_, err := os.Stat(filepath.Join(tmpDir, d))
			assert.True(t, os.IsNotExist(err))
		}
	})

	t.Run("non-existent dirs causes no error", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := rmMultipleDirs(tmpDir, []string{"ghost"})
		assert.NoError(t, err)
	})

	t.Run("empty dirs list", func(t *testing.T) {
		tmpDir := t.TempDir()
		err := rmMultipleDirs(tmpDir, []string{})
		assert.NoError(t, err)
	})
}

// Tests for mapMountFlag() - maps mount option string to flag struct
func TestMapMountFlag(t *testing.T) {
	t.Run("known flag bind returns correct value", func(t *testing.T) {
		result, exists := mapMountFlag("bind")
		assert.True(t, exists)
		assert.False(t, result.clear)
	})

	t.Run("known flag ro returns correct value", func(t *testing.T) {
		result, exists := mapMountFlag("ro")
		assert.True(t, exists)
		assert.False(t, result.clear)
	})

	t.Run("unknown flag returns false", func(t *testing.T) {
		_, exists := mapMountFlag("unknownflag")
		assert.False(t, exists)
	})

	t.Run("defaults flag exists", func(t *testing.T) {
		_, exists := mapMountFlag("defaults")
		assert.True(t, exists)
	})
}

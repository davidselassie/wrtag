package ripfile

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFind(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "notes.txt"), "just some notes")
	writeFile(t, filepath.Join(dir, "Disc 1", "Brave New World.log"), eacLog)
	writeFile(t, filepath.Join(dir, "Disc 1", "Brave New World (again).log"), eacLog)
	writeFile(t, filepath.Join(dir, "Disc 2", "Other.LOG"), otherDiscLog)
	require.NoError(t, os.Mkdir(filepath.Join(dir, "dir.log"), 0o700))

	discs, err := Find(t.Context(), dir)
	require.NoError(t, err)
	assert.Equal(t, []Disc{braveNewWorld, otherDisc}, discs)
}

func TestFindNone(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "error.log"), "not a rip log")

	discs, err := Find(t.Context(), dir)
	require.NoError(t, err)
	assert.Empty(t, discs)
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o700))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o600))
}

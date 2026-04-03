package memories

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStoreSaveAndLoad(t *testing.T) {
	store := NewStoreWithBase(t.TempDir())

	m := NewMemory("test-save", "A saved memory", "feedback", "proj", "Content here.")
	require.NoError(t, store.Save(m))

	loaded, err := store.Load("test-save")
	require.NoError(t, err)
	assert.Equal(t, "test-save", loaded.Name)
	assert.Equal(t, "feedback", loaded.Type)
	assert.Equal(t, "Content here.", loaded.Content)
}

func TestStoreSaveNoName(t *testing.T) {
	store := NewStoreWithBase(t.TempDir())
	m := &Memory{Type: "user", Content: "no name"}
	err := store.Save(m)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name is required")
}

func TestStoreLoadNotFound(t *testing.T) {
	store := NewStoreWithBase(t.TempDir())
	_, err := store.Load("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestStoreDelete(t *testing.T) {
	store := NewStoreWithBase(t.TempDir())

	m := NewMemory("to-delete", "Will be deleted", "user", "", "bye")
	require.NoError(t, store.Save(m))

	require.NoError(t, store.Delete("to-delete"))

	_, err := store.Load("to-delete")
	assert.Error(t, err)
}

func TestStoreDeleteNotFound(t *testing.T) {
	store := NewStoreWithBase(t.TempDir())
	err := store.Delete("nope")
	assert.Error(t, err)
}

func TestStoreList(t *testing.T) {
	store := NewStoreWithBase(t.TempDir())

	store.Save(NewMemory("alpha", "First", "user", "", "a"))
	store.Save(NewMemory("beta", "Second", "feedback", "", "b"))
	store.Save(NewMemory("gamma", "Third", "project", "", "c"))

	memories, err := store.List()
	require.NoError(t, err)
	assert.Len(t, memories, 3)
}

func TestStoreListEmpty(t *testing.T) {
	store := NewStoreWithBase(t.TempDir())
	memories, err := store.List()
	require.NoError(t, err)
	assert.Empty(t, memories)
}

func TestStoreListByType(t *testing.T) {
	store := NewStoreWithBase(t.TempDir())

	store.Save(NewMemory("a", "A", "feedback", "", "x"))
	store.Save(NewMemory("b", "B", "user", "", "y"))
	store.Save(NewMemory("c", "C", "feedback", "", "z"))

	feedback, err := store.ListByType("feedback")
	require.NoError(t, err)
	assert.Len(t, feedback, 2)

	users, err := store.ListByType("user")
	require.NoError(t, err)
	assert.Len(t, users, 1)

	empty, err := store.ListByType("project")
	require.NoError(t, err)
	assert.Empty(t, empty)
}

func TestSanitizeFilename(t *testing.T) {
	assert.Equal(t, "hello-world", sanitizeFilename("Hello World"))
	assert.Equal(t, "test-123", sanitizeFilename("test 123"))
	assert.Equal(t, "nospecialchars", sanitizeFilename("no!@#special$%^chars"))
	assert.Equal(t, "a-b", sanitizeFilename("a--b"))
}

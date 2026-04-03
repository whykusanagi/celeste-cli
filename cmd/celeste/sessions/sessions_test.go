package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// ---------- Writer tests ----------

func TestWriterCreatesFile(t *testing.T) {
	dir := t.TempDir()
	w, err := NewSessionWriter(dir, "test-1")
	if err != nil {
		t.Fatalf("NewSessionWriter: %v", err)
	}
	defer w.Close()

	if _, err := os.Stat(w.Path()); err != nil {
		t.Fatalf("expected file at %s: %v", w.Path(), err)
	}
	if filepath.Ext(w.Path()) != ".jsonl" {
		t.Fatalf("expected .jsonl extension, got %s", w.Path())
	}
}

func TestWriterAppendsEntries(t *testing.T) {
	dir := t.TempDir()
	w, err := NewSessionWriter(dir, "test-append")
	if err != nil {
		t.Fatal(err)
	}

	ts := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	entries := []LogEntry{
		{Type: "user", Content: "hello", Timestamp: ts},
		{Type: "assistant", Content: "hi there", Timestamp: ts.Add(time.Second)},
	}
	for _, e := range entries {
		if err := w.WriteEntry(e); err != nil {
			t.Fatalf("WriteEntry: %v", err)
		}
	}
	w.Close()

	got, err := ReadSession(w.Path())
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].Content != "hello" || got[1].Content != "hi there" {
		t.Fatalf("content mismatch: %+v", got)
	}
}

func TestWriterFiltersEphemeral(t *testing.T) {
	dir := t.TempDir()
	w, err := NewSessionWriter(dir, "test-ephemeral")
	if err != nil {
		t.Fatal(err)
	}

	ts := time.Now()
	w.WriteEntry(LogEntry{Type: "user", Content: "real", Timestamp: ts})
	w.WriteEntry(LogEntry{Type: "progress", Content: "loading...", Timestamp: ts})
	w.WriteEntry(LogEntry{Type: "typing", Content: "...", Timestamp: ts})
	w.WriteEntry(LogEntry{Type: "ping", Content: "", Timestamp: ts})
	w.WriteEntry(LogEntry{Type: "assistant", Content: "reply", Timestamp: ts})
	w.Close()

	got, err := ReadSession(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 non-ephemeral entries, got %d", len(got))
	}
}

func TestWriterToolCallFields(t *testing.T) {
	dir := t.TempDir()
	w, err := NewSessionWriter(dir, "test-tool")
	if err != nil {
		t.Fatal(err)
	}

	ts := time.Now()
	w.WriteEntry(LogEntry{
		Type:      "tool_call",
		Content:   `{"cmd":"ls"}`,
		ToolName:  "bash",
		ToolID:    "call_123",
		Timestamp: ts,
	})
	w.Close()

	got, err := ReadSession(w.Path())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatal("expected 1 entry")
	}
	if got[0].ToolName != "bash" || got[0].ToolID != "call_123" {
		t.Fatalf("tool fields mismatch: %+v", got[0])
	}
}

// ---------- Reader tests ----------

func TestReadSessionSkipsMalformed(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "bad.jsonl")
	data := `{"type":"user","content":"ok","timestamp":"2026-03-31T00:00:00Z"}
NOT JSON
{"type":"assistant","content":"reply","timestamp":"2026-03-31T00:00:01Z"}
`
	os.WriteFile(p, []byte(data), 0644)

	got, err := ReadSession(p)
	if err != nil {
		t.Fatalf("ReadSession: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries (skipping malformed), got %d", len(got))
	}
}

func TestReadSessionEmptyFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "empty.jsonl")
	os.WriteFile(p, []byte(""), 0644)

	got, err := ReadSession(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("expected 0 entries, got %d", len(got))
	}
}

func TestReadSessionFileNotFound(t *testing.T) {
	_, err := ReadSession("/nonexistent/path.jsonl")
	if err == nil {
		t.Fatal("expected error for missing file")
	}
}

func TestListSessions(t *testing.T) {
	dir := t.TempDir()

	// Create two session files with different mtimes.
	write := func(id, content string) {
		w, _ := NewSessionWriter(dir, id)
		w.WriteEntry(LogEntry{Type: "user", Content: content, Timestamp: time.Now()})
		w.WriteEntry(LogEntry{Type: "assistant", Content: "reply", Timestamp: time.Now()})
		w.Close()
	}

	write("sess-old", "first session question")
	// Ensure different mtime.
	time.Sleep(50 * time.Millisecond)
	write("sess-new", "second session question")

	infos, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("expected 2 sessions, got %d", len(infos))
	}
	// Newest first.
	if infos[0].ID != "sess-new" {
		t.Fatalf("expected sess-new first, got %s", infos[0].ID)
	}
	if infos[0].EntryCount != 2 {
		t.Fatalf("expected 2 entries, got %d", infos[0].EntryCount)
	}
	if infos[0].Title != "second session question" {
		t.Fatalf("unexpected title: %s", infos[0].Title)
	}
}

func TestListSessionsEmpty(t *testing.T) {
	dir := t.TempDir()
	infos, err := ListSessions(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 0 {
		t.Fatalf("expected 0, got %d", len(infos))
	}
}

// ---------- Manager tests ----------

func TestManagerStartAndLog(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{sessionDir: dir, projectID: "test"}

	if err := m.StartSession(); err != nil {
		t.Fatal(err)
	}
	defer m.Close()

	if m.SessionID() == "" {
		t.Fatal("expected non-empty session ID")
	}

	if err := m.LogTurn("user", "hello"); err != nil {
		t.Fatal(err)
	}
	if err := m.LogTurn("assistant", "hi"); err != nil {
		t.Fatal(err)
	}

	// Read back.
	p := filepath.Join(dir, m.SessionID()+".jsonl")
	entries, err := ReadSession(p)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}
}

func TestManagerLogTurnWithoutSession(t *testing.T) {
	m := &Manager{sessionDir: t.TempDir(), projectID: "test"}
	err := m.LogTurn("user", "no session")
	if err == nil {
		t.Fatal("expected error when no session is active")
	}
}

func TestManagerGetRecentSession(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{sessionDir: dir, projectID: "test"}

	// No sessions yet.
	info, err := m.GetRecentSession(4 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Fatal("expected nil when no sessions exist")
	}

	// Create a session.
	m.StartSession()
	m.LogTurn("user", "hello")
	m.Close()

	// Should find it.
	info, err = m.GetRecentSession(4 * time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	if info == nil {
		t.Fatal("expected recent session")
	}

	// With a very short maxAge, it should NOT find it (file was just written).
	// We can't reliably test "too old" without manipulating file times, so
	// we just confirm the branch works with 0 duration.
	info, err = m.GetRecentSession(0)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Fatal("expected nil with 0 maxAge")
	}
}

func TestManagerResumeSession(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{sessionDir: dir, projectID: "test"}

	// Create and populate a session.
	m.StartSession()
	id := m.SessionID()
	m.LogTurn("user", "original message")
	m.LogTurn("assistant", "original reply")
	m.Close()

	// Resume it.
	entries, err := m.ResumeSession(id)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries on resume, got %d", len(entries))
	}

	// Append more.
	m.LogTurn("user", "follow-up")
	m.Close()

	// Verify all 3 entries.
	all, _ := ReadSession(filepath.Join(dir, id+".jsonl"))
	if len(all) != 3 {
		t.Fatalf("expected 3 entries after resume+append, got %d", len(all))
	}
}

func TestManagerListSessions(t *testing.T) {
	dir := t.TempDir()
	m := &Manager{sessionDir: dir, projectID: "test"}

	m.StartSession()
	m.LogTurn("user", "one")
	m.Close()

	sessions, err := m.ListSessions()
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1, got %d", len(sessions))
	}
}

func TestNewManager(t *testing.T) {
	// Just verify it doesn't error on a real directory.
	m, err := NewManager(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if m.ProjectID() == "" {
		t.Fatal("expected non-empty project ID")
	}
	if m.SessionDir() == "" {
		t.Fatal("expected non-empty session dir")
	}
}

func TestProjectHashDeterministic(t *testing.T) {
	h1 := projectHash("/some/path")
	h2 := projectHash("/some/path")
	if h1 != h2 {
		t.Fatalf("hash not deterministic: %s vs %s", h1, h2)
	}
	h3 := projectHash("/other/path")
	if h1 == h3 {
		t.Fatal("different paths should produce different hashes")
	}
}

func TestTruncateTitle(t *testing.T) {
	cases := []struct {
		in     string
		maxLen int
		want   string
	}{
		{"short", 60, "short"},
		{"hello\nworld", 60, "hello world"},
		{"this is a very long message that should be truncated at a word boundary somewhere", 30, "this is a very long message..."},
		{"", 60, ""},
	}
	for _, c := range cases {
		got := truncateTitle(c.in, c.maxLen)
		if got != c.want {
			t.Errorf("truncateTitle(%q, %d) = %q, want %q", c.in, c.maxLen, got, c.want)
		}
	}
}

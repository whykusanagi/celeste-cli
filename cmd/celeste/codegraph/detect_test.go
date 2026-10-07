package codegraph

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		file     string
		expected string
	}{
		{"main.go", "go"},
		{"app.py", "python"},
		{"index.js", "javascript"},
		{"server.ts", "typescript"},
		{"component.tsx", "typescript"},
		{"lib.rs", "rust"},
		{"Main.java", "java"},
		{"style.css", "css"},
		{"page.html", "html"},
		{"config.yaml", "yaml"},
		{"data.json", "json"},
		{"README.md", "markdown"},
		{"unknown.xyz", ""},
	}

	for _, tt := range tests {
		t.Run(tt.file, func(t *testing.T) {
			assert.Equal(t, tt.expected, DetectLanguage(tt.file))
		})
	}
}

func TestDetectProjectLanguage(t *testing.T) {
	dir := t.TempDir()

	// Create a go.mod file
	os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module test"), 0644)
	assert.Equal(t, "go", DetectProjectLanguage(dir))

	// Create a package.json
	dir2 := t.TempDir()
	os.WriteFile(filepath.Join(dir2, "package.json"), []byte("{}"), 0644)
	assert.Equal(t, "javascript", DetectProjectLanguage(dir2))
}

func TestIsIndexableFile(t *testing.T) {
	assert.True(t, IsIndexableFile("main.go"))
	assert.True(t, IsIndexableFile("app.py"))
	assert.True(t, IsIndexableFile("server.ts"))
	// tree-sitter grammars ship for these; the indexer must reach them.
	for _, f := range []string{"Main.java", "lib.c", "lib.h", "a.cpp", "a.cc", "a.cxx", "a.hpp", "app.rb"} {
		assert.Truef(t, IsIndexableFile(f), "%s should be indexable", f)
	}
	assert.Equal(t, "cpp", DetectLanguage("a.cxx"))
	assert.False(t, IsIndexableFile("image.png"))
	assert.False(t, IsIndexableFile("data.bin"))
	assert.False(t, IsIndexableFile(".gitignore"))
}

func TestShouldSkipPath_VendorDirs(t *testing.T) {
	tests := []struct {
		path string
		skip bool
	}{
		{".", false},
		{"main.go", false},
		{"src/main.go", false},
		{"node_modules", true},
		{"node_modules/pkg/index.js", true},
		{"venv", true},
		{"venv/lib/site.py", true},
		{".venv", true},
		{".git", true},
		{"__pycache__", true},
		{".mypy_cache", true},
		{".pytest_cache", true},
		{".next", true},
		{".nuxt", true},
		{"target", true},
		{"env", true},
		{".celeste", true},
		{"vendor", true},
		{"dist", true},
		{"build", true},
		{".tox", true},
		{".cache", true},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			assert.Equal(t, tt.skip, ShouldSkipPath(tt.path), "ShouldSkipPath(%q)", tt.path)
		})
	}
}

func TestGitignoreFilter(t *testing.T) {
	dir := t.TempDir()

	// Write a .gitignore
	os.WriteFile(filepath.Join(dir, ".gitignore"), []byte(`
# Comment line
*.log
*.pyc
temp/
!important.log
`), 0644)

	filter := LoadGitignore(dir)
	assert.NotNil(t, filter)

	// *.log should match files
	assert.True(t, filter.ShouldSkip("debug.log", false))
	assert.True(t, filter.ShouldSkip("sub/error.log", false))

	// *.pyc should match
	assert.True(t, filter.ShouldSkip("module.pyc", false))

	// temp/ is a directory pattern
	assert.True(t, filter.ShouldSkip("temp", true))
	assert.False(t, filter.ShouldSkip("temp", false)) // file named temp, not dir

	// !important.log negates *.log for this specific file
	assert.False(t, filter.ShouldSkip("important.log", false))

	// Unmatched files should not be skipped
	assert.False(t, filter.ShouldSkip("main.go", false))
	assert.False(t, filter.ShouldSkip("src/app.py", false))
}

func TestGitignoreFilter_Nil(t *testing.T) {
	// No .gitignore file
	filter := LoadGitignore(t.TempDir())
	assert.Nil(t, filter)

	// Nil filter should not skip anything
	assert.False(t, filter.ShouldSkip("anything.go", false))
	assert.False(t, filter.ShouldSkip("node_modules", true))
}

// Review of #381: a .h header is C++ when it uses C++-only syntax outside
// comments and literals; other headers, and other extensions, are not.
func TestIsCppHeader(t *testing.T) {
	for _, tc := range []struct {
		name, src string
		want      bool
	}{
		{"x.h", "namespace geo { int f(); }\n", true},
		{"x.h", "class Shape {\npublic:\n  int f();\n};\n", true},
		{"x.h", "template <typename T> T pick();\n", true},
		{"x.h", "struct S { virtual int f(); };\n", true},
		{"x.h", "int geo::f();\n", true},
		{"X.H", "namespace geo {}\n", true},
		{"x.h", "#ifdef __cplusplus\nextern \"C\" {\n#endif\nint add(int a, int b);\n", false},
		{"x.h", "/* a class of helpers; see geo::f */\n// namespace note\nint add(int, int);\n", false},
		{"x.h", "const char *s = \"std::string\";\nint class_count;\n", false},
		// C23 attributes and GNU attributes are C too.
		{"x.h", "[[gnu::unused]] static int x;\n[[deprecated]] int old(void);\n", false},
		{"x.h", "struct S { int a; } __attribute__((packed));\nint f(int) __attribute__((nonnull(1)));\n", false},
		{"x.h", "struct S {\n  int (*cb)(void *);\n  char buf[N(3)];\n  LIST_ENTRY(S) link;\n};\nstruct S *make(int n);\n", false},
		{"x.h", "typedef struct {\n  void (*free)(void *);\n} ops;\n", false},
		// A struct with a member function, constructor or destructor is C++.
		{"x.h", "struct S { int f(); };\n", true},
		{"x.h", "struct S {\n  S();\n  int a;\n};\n", true},
		{"x.h", "struct S {\n  ~S();\n};\n", true},
		{"x.h", "struct S {\n  int area() const { return 1; }\n};\n", true},
		{"x.h", "[[nodiscard]] int f();\nint geo::g();\n", true},
		{"x.hpp", "namespace geo {}\n", false},
		{"x.c", "namespace geo {}\n", false},
	} {
		if got := IsCppHeader(tc.name, []byte(tc.src)); got != tc.want {
			t.Errorf("IsCppHeader(%q, %q) = %v, want %v", tc.name, tc.src, got, tc.want)
		}
	}
}

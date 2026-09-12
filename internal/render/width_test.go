package render

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode"

	"github.com/mattn/go-runewidth"
)

// Every glyph this program can draw must occupy exactly one cell.
//
// A double-width glyph shifts every column after it and nothing says so: the
// rows stay the right number of runes, the artifact stays the right length, and
// only a person looking at a terminal sees the damage. ⛅ and ⚡ are Wide, which
// is why the weather line draws ☁ and ⛆.
//
// This measures rather than lists. A list catches the two glyphs TRAPS.md
// already names, and the glyph that breaks alignment is the one nobody thought
// of, so the test reads every string and rune literal the program can emit and
// measures each character in it.
func TestEveryGlyphTheProgramCanDrawIsOneCell(t *testing.T) {
	t.Parallel()

	files := sourceFiles(t, "..")
	if len(files) < 6 {
		t.Fatalf("found %d source files to read, want the whole of internal/", len(files))
	}

	checked := 0
	for _, path := range files {
		for _, lit := range literalsIn(t, path) {
			for _, r := range lit.text {
				if r < unicode.MaxASCII {
					continue
				}
				checked++
				if w := runewidth.RuneWidth(r); w != 1 {
					t.Errorf("%s:%d draws %q (U+%04X), which is %d cells wide",
						path, lit.line, r, r, w)
				}
			}
		}
	}
	if checked == 0 {
		t.Error("measured no glyphs at all, so this test proves nothing")
	}
	t.Logf("measured %d glyph occurrences across %d files", checked, len(files))
}

type literal struct {
	text string
	line int
}

// sourceFiles is every non-test Go file under dir. A test file may hold a wide
// character on purpose, to prove one is rejected.
func sourceFiles(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		out = append(out, path)
		return nil
	})
	if err != nil {
		t.Fatalf("reading the source: %v", err)
	}
	return out
}

// literalsIn is every string and rune literal in one file. Comments are left
// out: a comment is not drawn, and several of them name the Wide glyphs this
// program avoids.
func literalsIn(t *testing.T, path string) []literal {
	t.Helper()
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, path, nil, 0)
	if err != nil {
		t.Fatalf("parsing %s: %v", path, err)
	}

	var out []literal
	ast.Inspect(file, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || (lit.Kind != token.STRING && lit.Kind != token.CHAR) {
			return true
		}
		text, err := strconv.Unquote(lit.Value)
		if err != nil {
			// A raw string with a backquote inside, or a rune literal Unquote
			// will not take. Measure the source text instead, which is a
			// superset of what it can draw.
			text = lit.Value
		}
		out = append(out, literal{text: text, line: fset.Position(lit.Pos()).Line})
		return true
	})
	return out
}

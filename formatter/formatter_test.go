package formatter

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/google/go-jsonnet/internal/testutils"
)

var update = flag.Bool("update", false, "update .golden files")

type formatterTest struct {
	name       string
	input      string
	goldenPath string
}

func mustReadFile(t *testing.T, file string) []byte {
	t.Helper()
	bytz, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("reading file: %s: %v", file, err)
	}
	return bytz
}

func coalesceError(out string, err error) (string, bool) {
	if err != nil {
		return err.Error(), false
	}
	return out, true
}

func TestFormatter(t *testing.T) {
	flag.Parse()

	var tests []*formatterTest

	match, err := filepath.Glob("testdata/*.jsonnet")
	if err != nil {
		t.Fatal(err)
	}

	jsonnetExtRE := regexp.MustCompile(`\.jsonnet$`)

	for _, input := range match {
		// Skip escaped filenames.
		if strings.ContainsRune(input, '%') {
			continue
		}
		name := jsonnetExtRE.ReplaceAllString(input, "")
		golden := jsonnetExtRE.ReplaceAllString(input, ".fmt.golden")
		tests = append(tests, &formatterTest{
			name:       name,
			input:      string(mustReadFile(t, input)),
			goldenPath: golden,
		})
	}

	var changedGoldens []string
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out, _ := coalesceError(Format(test.name, test.input, DefaultOptions()))
			if *update {
				changed, err := testutils.UpdateGoldenFile(test.goldenPath, []byte(out), 0666)
				if err != nil {
					t.Error(err)
				}
				if changed {
					changedGoldens = append(changedGoldens, test.goldenPath)
				}
			} else {
				golden := mustReadFile(t, test.goldenPath)
				if diff, hasDiff := testutils.CompareWithGolden(out, golden); hasDiff {
					t.Error(fmt.Errorf("golden file for %v has diff:\n%v", test.name, diff))
				}
			}
		})
	}

	if *update {
		// Little hack: a failed test which prints update stats.
		t.Run("Goldens Updated", func(t *testing.T) {
			t.Logf("Expected failure, for printing update stats. Does not appear without `-update`.")
			t.Logf("%d formatter goldens updated:\n", len(changedGoldens))
			for _, golden := range changedGoldens {
				t.Log(golden)
			}
			t.Fail()
		})
	}
}

func TestFormatNoImplicitPlus(t *testing.T) {
	flag.Parse()

	// Regression test for
	// https://github.com/google/go-jsonnet/issues/809#issuecomment-3897434856
	//
	// (No support yet for custom formatter config/flags in the testdata/ driven tests)

	// input evaluates to 9801
	// $ go run ./cmd/jsonnet -e '{ f(x):: x * x } { a: 1 }.f(99)'
	// 9801
	//
	// want evaluates to 9801
	// $ go run ./cmd/jsonnet -e '({ f(x):: x * x } + { a: 1 }).f(99)'
	// 9801
	//
	// Before fixing the bug, the formatter output is missing the necessary parentheses,
	// (which must be injected; they're not in the input), without the parens the
	// incorrectly formatted expression evaluates to:
	// $ go run ./cmd/jsonnet -e '{ f(x):: x * x } + { a: 1 }.f(99)'
	// RUNTIME ERROR: Field does not exist: f
	//     <cmdline>:1:20-30    $
	//     During evaluation
	//
	// exit status 1

	tests := []struct {
		name   string
		input  string
		golden string
	}{
		{
			name:   "implicit-plus-apply",
			input:  "{ f(x):: x * x } { a: 1 }.f(99)\n",
			golden: "({ f(x):: x * x } + { a: 1 }).f(99)\n",
		},
		{
			name:   "implicit-plus-index",
			input:  "{ a: 1 } { b: 2 }.a\n",
			golden: "({ a: 1 } + { b: 2 }).a\n",
		},
		{
			name:   "implicit-plus",
			input:  "{ a: 1 } { b: 2 }\n",
			golden: "{ a: 1 } + { b: 2 }\n",
		},
		{
			name:   "implicit-plus-three",
			input:  "{ a: 1 } { b: 2 } { c: 3 }\n",
			golden: "{ a: 1 } + { b: 2 } + { c: 3 }\n",
		},
		{
			name:   "implicit-plus-left",
			input:  "{ a: 1 } + { b: 2 } { c: 3 }\n",
			golden: "{ a: 1 } + ({ b: 2 } + { c: 3 })\n",
		},
		{
			name:   "implicit-plus-right",
			input:  "{ a: 1 } { b: 2 } + { c: 3 }\n",
			golden: "{ a: 1 } + { b: 2 } + { c: 3 }\n",
		},
		{
			name:   "implicit-plus-unary",
			input:  "+ 42 { b: 2 }\n",
			golden: "+(42 + { b: 2 })\n",
		},
		{
			name:   "implicit-plus-mul-right",
			input:  "{ a: 1 } * { b: 2 } { c: 3 }\n",
			golden: "{ a: 1 } * ({ b: 2 } + { c: 3 })\n",
		},
		{
			name:   "implicit-plus-mul-left",
			input:  "{ a: 1 } { b: 2 } * { c: 3 }\n",
			golden: "({ a: 1 } + { b: 2 }) * { c: 3 }\n",
		},
	}

	opts := DefaultOptions()
	opts.UseImplicitPlus = false

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			out, _ := coalesceError(Format(test.name, test.input, opts))
			t.Logf("formatter test\n%q\nformats to\n%q\n", test.input, out)
			if diff, hasDiff := testutils.CompareWithGolden(out, []byte(test.golden)); hasDiff {
				t.Error(fmt.Errorf("golden file for %v has diff:\n%v", test.name, diff))
			}
		})
	}
}

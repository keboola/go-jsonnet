package jsonnet

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/go-jsonnet/ast"
	"github.com/google/go-jsonnet/internal/program"
	"github.com/stretchr/testify/assert"
)

type errorFormattingTest struct {
	name      string
	input     string
	errString string
}

func genericTestErrorMessage(t *testing.T, tests []errorFormattingTest, format func(RuntimeError) string) {
	for _, test := range tests {
		vm := MakeVM()
		rawOutput, err := vm.evaluateSnippet(ast.DiagnosticFileName(test.name), "", test.input, evalKindRegular)
		var errString string
		if err != nil {
			switch typedErr := err.(type) {
			case RuntimeError:
				errString = format(typedErr)
			default:
				t.Errorf("%s: unexpected error: %v", test.name, err)
			}
		}
		output := rawOutput.(string)
		if errString != test.errString {
			t.Errorf("%s: error result does not match. got\n\t%+#v\nexpected\n\t%+#v",
				test.name, errString, test.errString)
		}
		if err == nil {
			t.Errorf("%s, Expected error, but execution succeded and the here's the result:\n %v\n", test.name, output)
		}
	}
}

// TODO(sbarzowski) Perhaps we should have just one set of tests with all the variants?
// TODO(sbarzowski) Perhaps this should be handled in external tests?
var oneLineTests = []errorFormattingTest{
	{"error", `error "x"`, "RUNTIME ERROR: x"},
}

func TestOneLineError(t *testing.T) {
	genericTestErrorMessage(t, oneLineTests, func(r RuntimeError) string {
		return r.Error()
	})
}

// TODO(sbarzowski) checking if the whitespace is right is quite unpleasant, what can we do about it?
var minimalErrorTests = []errorFormattingTest{
	{"error", `error "x"`, "RUNTIME ERROR: x\n" +
		"	error:1:1-10	$\n" + // TODO(sbarzowski) if seems we have off-by-one in location
		"	During evaluation	\n" +
		""},
	{"error_in_func", `local x(n) = if n == 0 then error "x" else x(n - 1); x(3)`, "RUNTIME ERROR: x\n" +
		"	error_in_func:1:29-38	function <x>\n" +
		"	error_in_func:1:44-52	function <x>\n" +
		"	error_in_func:1:44-52	function <x>\n" +
		"	error_in_func:1:44-52	function <x>\n" +
		"	error_in_func:1:54-58	$\n" +
		"	During evaluation	\n" +
		""},
	{"error_in_error", `error (error "x")`, "RUNTIME ERROR: x\n" +
		"	error_in_error:1:8-17	$\n" +
		"	During evaluation	\n" +
		""},
}

func TestMinimalError(t *testing.T) {
	formatter := termErrorFormatter{maxStackTraceSize: 20}
	genericTestErrorMessage(t, minimalErrorTests, func(r RuntimeError) string {
		return formatter.Format(r)
	})
}

// TODO(sbarzowski) test pretty errors once they are stable-ish
// probably "golden" pattern is the right one for that

func removeExcessiveWhitespace(s string) string {
	var buf bytes.Buffer
	needsSeparation := false
	for i, w := 0, 0; i < len(s); i += w {
		runeValue, width := utf8.DecodeRuneInString(s[i:])
		if runeValue == '\n' || runeValue == ' ' {
			needsSeparation = true
		} else {
			if needsSeparation {
				buf.WriteString(" ")
				needsSeparation = false
			}
			buf.WriteRune(runeValue)
		}
		w = width
	}
	return buf.String()
}

func TestCustomImporter(t *testing.T) {
	vm := MakeVM()
	vm.Importer(&MemoryImporter{
		map[string]Contents{
			"a.jsonnet": MakeContents("2 + 2"),
			"b.jsonnet": MakeContents("3 + 3"),
			"c.bin":     MakeContentsRaw([]byte{0xff, 0xfe, 0xfd}),
		},
	})
	input := `[import "a.jsonnet", importstr "b.jsonnet", importbin "c.bin"]`
	expected := `[ 4, "3 + 3", [ 255, 254, 253 ] ]`
	actual, err := vm.EvaluateSnippet("custom_import.jsonnet", input)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	actual = removeExcessiveWhitespace(actual)
	if actual != expected {
		t.Errorf("Expected %q, but got %q", expected, actual)
	}
}

type importHistoryEntry struct {
	importedFrom string
	importedPath string
}

type importerWithHistory struct {
	i       MemoryImporter
	history []importHistoryEntry
}

func (importer *importerWithHistory) Import(importedFrom, importedPath string) (contents Contents, foundAt string, err error) {
	importer.history = append(importer.history, importHistoryEntry{importedFrom, importedPath})
	return importer.i.Import(importedFrom, importedPath)
}

func TestExtVarImportedFrom(t *testing.T) {
	vm := MakeVM()
	vm.ExtCode("aaa", "import 'a.jsonnet'")
	importer := importerWithHistory{
		i: MemoryImporter{
			map[string]Contents{
				"a.jsonnet": MakeContents("2 + 2"),
			},
		},
	}
	vm.Importer(&importer)
	input := `std.extVar('aaa')`
	expected := `4`
	actual, err := vm.EvaluateSnippet("blah.jsonnet", input)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	actual = removeExcessiveWhitespace(actual)
	if actual != expected {
		t.Errorf("Expected %q, but got %q", expected, actual)
	}
	expectedImportHistory := []importHistoryEntry{{"", "a.jsonnet"}}
	if !reflect.DeepEqual(importer.history, expectedImportHistory) {
		t.Errorf("Expected %q, but got %q", expectedImportHistory, importer.history)
	}
}

func TestTLAImportedFrom(t *testing.T) {
	vm := MakeVM()
	vm.TLACode("aaa", "import 'a.jsonnet'")
	importer := importerWithHistory{
		i: MemoryImporter{
			map[string]Contents{
				"a.jsonnet": MakeContents("2 + 2"),
			},
		},
	}
	vm.Importer(&importer)
	input := `function(aaa) aaa`
	expected := `4`
	actual, err := vm.EvaluateSnippet("blah.jsonnet", input)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	actual = removeExcessiveWhitespace(actual)
	if actual != expected {
		t.Errorf("Expected %q, but got %q", expected, actual)
	}
	expectedImportHistory := []importHistoryEntry{{"", "a.jsonnet"}}
	if !reflect.DeepEqual(importer.history, expectedImportHistory) {
		t.Errorf("Expected %q, but got %q", expectedImportHistory, importer.history)
	}
}

func TestAnonymousImportedFrom(t *testing.T) {
	vm := MakeVM()
	importer := importerWithHistory{
		i: MemoryImporter{
			map[string]Contents{
				"a.jsonnet": MakeContents("2 + 2"),
			},
		},
	}
	vm.Importer(&importer)
	input := `import "a.jsonnet"`
	expected := `4`
	actual, err := vm.EvaluateAnonymousSnippet("blah.jsonnet", input)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	actual = removeExcessiveWhitespace(actual)
	if actual != expected {
		t.Errorf("Expected %q, but got %q", expected, actual)
	}
	expectedImportHistory := []importHistoryEntry{{"", "a.jsonnet"}}
	if !reflect.DeepEqual(importer.history, expectedImportHistory) {
		t.Errorf("Expected %q, but got %q", expectedImportHistory, importer.history)
	}
}

func TestContents(t *testing.T) {
	a := "aaa"
	c1 := MakeContents(a)
	a = "bbb"
	if c1.String() != "aaa" {
		t.Errorf("Contents should be immutable")
	}
	c2 := MakeContents(a)
	c3 := MakeContents(a)
	if c2 == c3 {
		t.Errorf("Contents should distinguish between different instances even if they have the same data inside")
	}
}

func TestExtReset(t *testing.T) {
	vm := MakeVM()
	vm.ExtVar("fooString", "bar")
	vm.ExtCode("fooCode", "true")
	_, err := vm.EvaluateAnonymousSnippet("test.jsonnet", `{ str: std.extVar('fooString'), code: std.extVar('fooCode') }`)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	vm.ExtReset()
	_, err = vm.EvaluateAnonymousSnippet("test.jsonnet", `{ str: std.extVat('fooString'), code: std.extVar('fooCode') }`)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Undefined external variable") {
		t.Errorf("unexpected error %v", err)
	}
}

func TestTLAReset(t *testing.T) {
	vm := MakeVM()
	vm.TLAVar("fooString", "bar")
	vm.TLACode("fooCode", "true")
	_, err := vm.EvaluateAnonymousSnippet("test.jsonnet", `function (fooString, fooCode) { str: fooString, code: fooCode }`)
	if err != nil {
		t.Fatalf("unexpected error %v", err)
	}
	vm.TLAReset()
	_, err = vm.EvaluateAnonymousSnippet("test.jsonnet", `function(fooString, fooCode) { str: fooString, code: fooCode }`)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Missing argument") {
		t.Errorf("unexpected error %v", err)
	}
}

func assertVarOutput(t *testing.T, jsonStr string) {
	var data struct {
		Var  string `json:"var"`
		Code string `json:"code"`
		Node string `json:"node"`
	}
	err := json.Unmarshal([]byte(jsonStr), &data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if data.Var != "var" {
		t.Errorf("var attribute not correct, want '%s' got '%s'", "var", data.Var)
	}
	if data.Code != "code" {
		t.Errorf("code attribute not correct, want '%s' got '%s'", "code", data.Code)
	}
	if data.Node != "node" {
		t.Errorf("node attribute not correct, want '%s' got '%s'", "node", data.Node)
	}
}

func TestExtTypes(t *testing.T) {
	node, err := SnippetToAST("var.jsonnet", `{ node: 'node' }`)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	vm := MakeVM()
	vm.ExtVar("var", "var")
	vm.ExtCode("code", `{ code: 'code'}`)
	vm.ExtNode("node", node)

	jsonStr, err := vm.EvaluateAnonymousSnippet(
		"caller.jsonnet",
		`{ var: std.extVar('var') } + std.extVar('code') + std.extVar('node')`,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	assertVarOutput(t, jsonStr)
}

func TestTLATypes(t *testing.T) {
	node, err := SnippetToAST("var.jsonnet", `{ node: 'node' }`)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	vm := MakeVM()
	vm.TLAVar("var", "var")
	vm.TLACode("code", `{ code: 'code'}`)
	vm.TLANode("node", node)

	jsonStr, err := vm.EvaluateAnonymousSnippet(
		"caller.jsonnet",
		`function (var, code, node) { var: var } + code + node`,
	)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	assertVarOutput(t, jsonStr)
}

func TestSetTraceOut(t *testing.T) {
	traceOut := &strings.Builder{}
	vm := MakeVM()
	vm.SetTraceOut(traceOut)

	const filename = "blah.jsonnet"
	const msg = "TestSetTraceOut Trace Message"
	expected := fmt.Sprintf("TRACE: %s:1 %s", filename, msg)
	input := fmt.Sprintf("std.trace('%s', 'rest')", msg)

	_, err := vm.EvaluateAnonymousSnippet(filename, input)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	actual := removeExcessiveWhitespace(traceOut.String())
	if actual != expected {
		t.Errorf("Expected %q, but got %q", expected, actual)
	}
}

func TestGlobalBinding(t *testing.T) {
	vm := MakeVM()
	vm.Bind("myVar", &ast.LiteralString{Value: "bar"})
	fileName := "main.jsonnet"
	snippet := `{foo: myVar}`
	expected := "{\n   \"foo\": \"bar\"\n}\n"

	// Analyze without global variable definition
	_, err := program.SnippetToAST(ast.DiagnosticFileName(fileName), fileName, snippet)
	expectedErr := "main.jsonnet:1:7-12 Unknown variable: myVar"
	if err == nil {
		t.Fatalf("Expected error, got nil.")
	}
	if err.Error() != expectedErr {
		t.Fatalf("Unexpected error, want '%v', got '%v'.", expectedErr, err)
	}

	// Analyze with global variable definition
	_, err = program.SnippetToAST(ast.DiagnosticFileName(fileName), fileName, snippet, "myVar")
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	// Evaluate
	actual, err := vm.EvaluateAnonymousSnippet(fileName, snippet)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if actual != expected {
		t.Errorf("output is not correct, want '%s' got '%s'", expected, actual)
	}

	// Evaluate with import
	importer := &MemoryImporter{Data: map[string]Contents{"import.jsonnet": MakeContents(snippet)}}
	vm.Importer(importer)
	actual, err = vm.EvaluateAnonymousSnippet(fileName, `import "import.jsonnet"`)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}
	if actual != expected {
		t.Errorf("output is not correct, want '%s' got '%s'", expected, actual)
	}
}

func TestNotifier_OnGeneratedValue(t *testing.T) {
	notifier := &testNotifier{}
	vm := MakeVM()
	vm.Notifier(notifier)

	// Function "decorate" wraps string value with "~".
	vm.NativeFunction(&NativeFunction{
		Name:   `decorate`,
		Params: ast.Identifiers{"str"},
		Func: func(params []interface{}) (interface{}, error) {
			return fmt.Sprintf("~%s~", params[0].(string)), nil
		},
	})

	// Function "keyValueObject" converts key and value to object.
	vm.NativeFunction(&NativeFunction{
		Name:   `keyValueObject`,
		Params: ast.Identifiers{"key", "value"},
		Func: func(params []interface{}) (interface{}, error) {
			return map[string]interface{}{params[0].(string): params[1].(string)}, nil
		},
	})

	// Input Jsonnet code.
	code := `
local Person(name='Alice') = {
  name: if true then std.native('decorate')(name) else null,
};
local Do() = {
  myObject: {
    mergedObject: 
		std.native('keyValueObject')("A", "AAA") +
		std.native('keyValueObject')("B", "BBB") +
		{
			"sub": std.native('keyValueObject')("C", "CCC")
		}
  },
  person1: Person(),
  person2: Person('Bob'),
  other: [Person('Foo'), Person('Bar')],
};
Do()
`

	// Expected output Json.
	expected := `{
   "myObject": {
      "mergedObject": {
         "A": "AAA",
         "B": "BBB",
         "sub": {
            "C": "CCC"
         }
      }
   },
   "other": [
      {
         "name": "~Foo~"
      },
      {
         "name": "~Bar~"
      }
   ],
   "person1": {
      "name": "~Alice~"
   },
   "person2": {
      "name": "~Bob~"
   }
}
`

	// Notified values:
	expectedNotifications := []generatedValue{
		// Objects merging
		{
			fnName:  "keyValueObject",
			args:    []interface{}{"C", "CCC"},
			partial: false,
			partialValue: map[string]interface{}{
				"C": "CCC",
			},
			finalValue: map[string]interface{}{
				"C": "CCC",
			},
			steps: []interface{}{
				ObjectFieldStep{Field: "myObject"},
				ObjectFieldStep{Field: "mergedObject"},
				ObjectFieldStep{Field: "sub"},
			},
		},
		{
			fnName:  "keyValueObject",
			args:    []interface{}{"A", "AAA"},
			partial: true,
			partialValue: map[string]interface{}{
				"A": "AAA",
			},
			finalValue: map[string]interface{}{
				"A": "AAA",
				"B": "BBB",
				"sub": map[string]interface{}{
					"C": "CCC",
				},
			},
			steps: []interface{}{
				ObjectFieldStep{Field: "myObject"},
				ObjectFieldStep{Field: "mergedObject"},
			},
		},
		{
			fnName:  "keyValueObject",
			args:    []interface{}{"B", "BBB"},
			partial: true,
			partialValue: map[string]interface{}{
				"B": "BBB",
			},
			finalValue: map[string]interface{}{
				"A": "AAA",
				"B": "BBB",
				"sub": map[string]interface{}{
					"C": "CCC",
				},
			},
			steps: []interface{}{
				ObjectFieldStep{Field: "myObject"},
				ObjectFieldStep{Field: "mergedObject"},
			},
		},
		// Simple usage
		{
			fnName:       "decorate",
			args:         []interface{}{"Foo"},
			partial:      false,
			partialValue: "~Foo~",
			finalValue:   "~Foo~",
			steps: []interface{}{
				ObjectFieldStep{Field: "other"},
				ArrayIndexStep{Index: 0},
				ObjectFieldStep{Field: "name"},
			},
		},
		{
			fnName:       "decorate",
			args:         []interface{}{"Bar"},
			partial:      false,
			partialValue: "~Bar~",
			finalValue:   "~Bar~",
			steps: []interface{}{
				ObjectFieldStep{Field: "other"},
				ArrayIndexStep{Index: 1},
				ObjectFieldStep{Field: "name"},
			},
		},
		{
			fnName:       "decorate",
			args:         []interface{}{"Alice"},
			partial:      false,
			partialValue: "~Alice~",
			finalValue:   "~Alice~",
			steps: []interface{}{
				ObjectFieldStep{Field: "person1"},
				ObjectFieldStep{Field: "name"},
			},
		},
		{
			fnName:       "decorate",
			args:         []interface{}{"Bob"},
			partial:      false,
			partialValue: "~Bob~",
			finalValue:   "~Bob~",
			steps: []interface{}{
				ObjectFieldStep{Field: "person2"},
				ObjectFieldStep{Field: "name"},
			},
		},
	}

	// Evaluate and assert
	actual, err := vm.EvaluateAnonymousSnippet("file", code)
	assert.NoError(t, err)
	assert.Equal(t, expected, actual)
	assert.Equal(t, expectedNotifications, notifier.values)
}

type testNotifier struct {
	values []generatedValue
}

type generatedValue struct {
	fnName       string
	args         []interface{}
	partial      bool
	partialValue interface{}
	finalValue   interface{}
	steps        []interface{}
}

func (n *testNotifier) OnGeneratedValue(fnName string, args []interface{}, partial bool, partialValue, finalValue interface{}, steps []interface{}) {
	n.values = append(n.values, generatedValue{
		fnName:       fnName,
		args:         args,
		partial:      partial,
		partialValue: partialValue,
		finalValue:   finalValue,
		steps:        steps,
	})
}

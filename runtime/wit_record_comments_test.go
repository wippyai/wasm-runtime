package runtime

import "testing"

func TestParseWitFunctionsIgnoresCommentedRecords(t *testing.T) {
	for name, source := range map[string]string{
		"line":              "// record sample { field: missing }\nf: func(x: u8) -> u8;",
		"block":             "/* record sample { field: missing } */ f: func(x: u8) -> u8;",
		"nested block":      "/* outer /* record sample { field: missing } */ end */ f: func(x: u8) -> u8;",
		"field comments":    "record sample { /* explanation */ field: u8, } f: func(x: sample) -> sample;",
		"quoted annotation": `@deprecated(note = "record sample { field: missing } /*") f: func(x: u8) -> u8;`,
	} {
		t.Run(name, func(t *testing.T) {
			functions, err := parseWitFunctions(source)
			if err != nil {
				t.Fatal(err)
			}
			if functions["f"] == nil {
				t.Fatal("function f was not parsed")
			}
		})
	}
}

package report_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// schemaDir is schema/ seen from this package directory, which is the working
// directory of the test binary.
const schemaDir = "../../schema"

const schemaFile = "report.v1.json"

func schemaPath() string { return filepath.Join(schemaDir, schemaFile) }

// TestReport_SchemaCoversEveryEmittedField guards the contract in the only
// direction that breaks an orchestrator: a field the renderer emits and the
// schema does not describe.
func TestReport_SchemaCoversEveryEmittedField(t *testing.T) {
	t.Parallel()

	emitted := map[string]struct{}{}
	collectJSONTags(reflect.TypeFor[domain.Report](), emitted)
	if len(emitted) == 0 {
		t.Fatal("domain.Report declared no json tags, the walk is broken")
	}

	declared := loadSchemaProperties(t)
	var missing []string
	for name := range emitted {
		if _, ok := declared[name]; !ok {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("%s does not declare emitted field(s): %s", schemaPath(), strings.Join(missing, ", "))
	}
}

// TestReport_SchemaDeclaresRequiredTopLevelFields pins the fields the
// orchestrator may read without a presence check.
func TestReport_SchemaDeclaresRequiredTopLevelFields(t *testing.T) {
	t.Parallel()

	doc := loadSchema(t)
	names, ok := doc["required"].([]any)
	if !ok {
		t.Fatalf("%s declares no top-level required list", schemaPath())
	}
	required := map[string]struct{}{}
	for _, v := range names {
		required[v.(string)] = struct{}{}
	}
	want := []string{
		"schema", "redfirst_version", "tier", "config_source",
		"base", "head", "verdict", "exit_code", "diff", "gates",
	}
	for _, name := range want {
		if _, ok := required[name]; !ok {
			t.Errorf("%s must require %q", schemaPath(), name)
		}
	}
	// Adding a field stays a non-breaking change, so the top level may not be
	// closed.
	if _, closed := doc["additionalProperties"]; closed {
		t.Error("the top level must not set additionalProperties")
	}
}

// TestReport_SchemaDeclaresFieldsTheCoreDoesNotEmitYet covers the keys the
// spec's JSON sample shows and S0 leaves out.
func TestReport_SchemaDeclaresFieldsTheCoreDoesNotEmitYet(t *testing.T) {
	t.Parallel()

	declared := loadSchemaProperties(t)
	for _, name := range []string{
		"diff_digest", "base_probe_key",
		"env_up_s", "base_probe_s", "head_probe_s", "suite_s", "base_suite_s",
	} {
		if _, ok := declared[name]; !ok {
			t.Errorf("%s must declare %q from the spec sample", schemaPath(), name)
		}
	}
}

func loadSchema(t *testing.T) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(schemaPath())
	if err != nil {
		t.Fatalf("read schema: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse schema: %v", err)
	}
	return doc
}

func loadSchemaProperties(t *testing.T) map[string]struct{} {
	t.Helper()
	declared := map[string]struct{}{}
	collectSchemaProperties(loadSchema(t), declared)
	return declared
}

// collectJSONTags walks the struct the renderer marshals and records every
// name that can reach the wire, nested structs and slice elements included.
func collectJSONTags(typ reflect.Type, into map[string]struct{}) {
	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
		typ = typ.Elem()
	}
	if typ.Kind() != reflect.Struct {
		return
	}
	for i := range typ.NumField() {
		f := typ.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		into[name] = struct{}{}
		collectJSONTags(f.Type, into)
	}
}

// collectSchemaProperties gathers the keys of every "properties" object in the
// document, wherever $defs put it.
func collectSchemaProperties(node any, into map[string]struct{}) {
	switch v := node.(type) {
	case map[string]any:
		if props, ok := v["properties"].(map[string]any); ok {
			for name := range props {
				into[name] = struct{}{}
			}
		}
		for _, child := range v {
			collectSchemaProperties(child, into)
		}
	case []any:
		for _, child := range v {
			collectSchemaProperties(child, into)
		}
	}
}

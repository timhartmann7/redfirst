package report_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/report"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

func TestReport_JSONMatchesGoldenPerTier(t *testing.T) {
	t.Parallel()
	for _, tc := range goldenCases() {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var buf bytes.Buffer
			if err := report.JSON(&buf, tc.report); err != nil {
				t.Fatalf("JSON: %v", err)
			}
			testkit.Golden(t, tc.name+".json", buf.Bytes())
		})
	}
}

func TestReport_JSONOmitsUnsetOptionalFields(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	if err := report.JSON(&buf, tier1Report()); err != nil {
		t.Fatalf("JSON: %v", err)
	}
	got := buf.String()
	for _, field := range []string{"env_mode", "probe_runs", "base_runs", "\"case\""} {
		if strings.Contains(got, field) {
			t.Errorf("tier 1 JSON should not carry %q\n---\n%s", field, got)
		}
	}
	if !strings.HasSuffix(got, "}\n") {
		t.Error("JSON report must end in a newline")
	}
}

func TestReport_RenderersReportWriterFailure(t *testing.T) {
	t.Parallel()
	boom := errors.New("disk full")
	tests := []struct {
		name   string
		render func(w *failWriter) error
	}{
		{"Text", func(w *failWriter) error { return report.Text(w, tier1Report()) }},
		{"JSON", func(w *failWriter) error { return report.JSON(w, tier1Report()) }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.render(&failWriter{err: boom})
			if !errors.Is(err, boom) {
				t.Fatalf("want the writer error wrapped, got %v", err)
			}
		})
	}
}

type failWriter struct{ err error }

func (f *failWriter) Write([]byte) (int, error) { return 0, f.err }

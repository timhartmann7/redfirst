package report

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// JSON renders the machine report, schema v1. The struct tags on domain.Report
// are the contract, so nothing here reshapes them.
func JSON(w io.Writer, r domain.Report) error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return fmt.Errorf("%w: marshal report: %w", domain.ErrInternal, err)
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return fmt.Errorf("write json report: %w", err)
	}
	return nil
}

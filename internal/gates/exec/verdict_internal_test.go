package exec

import (
	"testing"

	"github.com/timhartmann7/redfirst/internal/domain"
)

// TestJudgeHead_ACaseHeadNeverReportsIsNotTheSourcesFailing separates absence
// from red.
//
// The base probe named a case that no head run mentions. Telling the agent to
// fix the code sends it at sources that are doing their job: it is the name
// that did not survive. A harness reporting the file it could not build under a
// name of its own puts every honest fix that adds a module here, which is why
// the shipped hook sets decide the tree builds before they report anything.
func TestJudgeHead_ACaseHeadNeverReportsIsNotTheSourcesFailing(t *testing.T) {
	t.Parallel()

	base := domain.Runs{{Index: 1, Green: false, Cases: []domain.Case{
		{File: "src/total.test.js", Name: "src/total.test.js", Outcome: domain.CaseFail},
	}}}
	head := domain.Runs{{Index: 1, Green: true, Cases: []domain.Case{
		{File: "src/total.test.js", Name: "adds prices", Outcome: domain.CasePass},
	}}}

	got := judgeHead(base, head, []string{"src/total.test.js"})

	if got.Status != domain.StatusFail {
		t.Fatalf("status = %q, want a refusal: absent is not green", got.Status)
	}
	if got.Message != goneOnHead {
		t.Errorf("message = %q, want %q", got.Message, goneOnHead)
	}
	if got.Remediation != domain.RemediationFixTest {
		t.Errorf("remediation = %q, want %q: the sources are not what failed",
			got.Remediation, domain.RemediationFixTest)
	}
	if len(got.Evidence) != 1 || got.Evidence[0].File != "src/total.test.js" {
		t.Fatalf("evidence = %v, want the file the base probe attributed it to", got.Evidence)
	}
}

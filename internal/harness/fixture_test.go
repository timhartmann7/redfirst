package harness_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/timhartmann7/redfirst/internal/gitx"
	"github.com/timhartmann7/redfirst/internal/harness"
	"github.com/timhartmann7/redfirst/internal/testkit"
)

// hookSet names the testdata scripts a fixture repository carries under
// .redfirst/. An empty field leaves that hook out, which is how a project
// without services and how a project without a reset both look.
type hookSet struct {
	up    string
	reset string
	test  string
	env   string
}

// recording is the standard set: every hook writes down that it ran, and
// test.sh reports two named cases.
func recording() hookSet {
	return hookSet{up: "env-up.sh", reset: "env-reset.sh", test: "test-record.sh"}
}

// fixture is a repository whose base ref carries a hook set, plus the log those
// hooks append to. The log sits outside the run directory on purpose: the
// teardown removes everything else, and the teardown is what several of these
// tests are about.
type fixture struct {
	t    *testing.T
	repo *testkit.Repo
	log  string
}

func newFixture(t *testing.T, set hookSet) *fixture {
	t.Helper()

	f := &fixture{t: t, log: filepath.Join(t.TempDir(), "hooks.log")}
	f.repo = testkit.NewRepo(t)
	f.repo.Write("src/total.js", "export const total = () => 0\n")
	f.repo.Write("src/total.test.js", "test('adds prices', () => {})\n")

	f.install("env-up.sh", set.up)
	f.install("env-down.sh", "env-down.sh")
	f.install("env-reset.sh", set.reset)
	f.install("test.sh", set.test)
	f.install("env.sh", set.env)
	f.repo.Commit("chore: seed the tree and the hooks")

	// A head branch, so a phase can lay out a tree the base ref does not have.
	f.repo.Branch(testkit.FixtureHead)
	f.repo.Write("src/total.js", "export const total = (i) => i.length\n")
	f.repo.Commit("fix: count the items")
	f.repo.Checkout(testkit.FixtureBase)

	return f
}

// install copies one testdata script into the fixture, pointing it at this
// fixture's log.
func (f *fixture) install(name, source string) {
	f.t.Helper()

	if source == "" {
		return
	}
	data, err := os.ReadFile(filepath.Join("testdata", "hooks", source))
	if err != nil {
		f.t.Fatalf("read hook %s: %v", source, err)
	}
	script := strings.ReplaceAll(string(data), "@LOG@", shellQuote(f.log))
	f.repo.WriteScript(harness.HooksDir+"/"+name, script)
}

// gitRepo is the git adapter over the fixture, which is what the harness
// materialises its trees through.
func gitRepo(t *testing.T, f *fixture) *gitx.Repo {
	t.Helper()

	repo, err := gitx.Open(t.Context(), f.repo.Dir)
	if err != nil {
		t.Fatalf("open %s: %v", f.repo.Dir, err)
	}
	return repo
}

// open brings the session up the way the CLI would.
func (f *fixture) open(t *testing.T, fresh bool) *harness.Session {
	t.Helper()

	s, err := f.tryOpen(t, fresh)
	if err != nil {
		t.Fatalf("open the session: %v", err)
	}
	return s
}

func (f *fixture) tryOpen(t *testing.T, fresh bool) (*harness.Session, error) {
	t.Helper()

	return harness.Open(t.Context(), harness.Options{
		Source:  gitRepo(t, f),
		Base:    testkit.FixtureBase,
		WorkDir: t.TempDir(),
		Fresh:   fresh,
	})
}

// lines is what the hooks wrote, in the order they ran.
func (f *fixture) lines() []string {
	f.t.Helper()

	data, err := os.ReadFile(f.log)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		f.t.Fatalf("read the hook log: %v", err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// count is how many logged lines open with prefix.
func (f *fixture) count(prefix string) int {
	f.t.Helper()

	n := 0
	for _, l := range f.lines() {
		if strings.HasPrefix(l, prefix) {
			n++
		}
	}
	return n
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

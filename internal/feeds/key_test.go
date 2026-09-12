package feeds

import (
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Where the subscription key comes from. Nothing here reaches a feed: what is
// worth testing is which place wins and which spellings are accepted, and both
// are decided before any request is made.
//
// Every test here isolates first. Key reads the machine, and a real key lives on
// the machine this is developed on: a test that did not isolate would find it,
// print it in a failure, and pass or fail for a reason that has nothing to do
// with the test.

// isolated gives a test an environment and a directory of its own, with no key
// in either.
func isolated(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	for _, name := range keyNames {
		t.Setenv(name, "")
	}
	t.Chdir(dir)
	return dir
}

func TestTheEnvironmentIsAskedFirst(t *testing.T) {
	// Not parallel: it sets the environment, which every test in this process
	// shares.
	dir := isolated(t)
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SUBSCRIPTION_KEY=from-a-file\n"), 0o600); err != nil {
		t.Fatalf("writing the file: %v", err)
	}
	t.Setenv("OC_TRANSPO_SUBSCRIPTION_KEY", "from-the-environment")
	if got := Key(); got != "from-the-environment" {
		t.Errorf("Key = %q, want the environment's", got)
	}
}

func TestEveryNameThisKeyHasEverHadIsAccepted(t *testing.T) {
	// A key file written for either implementation of this program has to work
	// for the other, and the name has changed three times.
	for _, name := range keyNames {
		t.Run(name, func(t *testing.T) {
			isolated(t)
			t.Setenv(name, "a-key")
			if got := Key(); got != "a-key" {
				t.Errorf("a key under %s read as %q", name, got)
			}
		})
	}
}

func TestTheTemplatesStandInIsNotAKey(t *testing.T) {
	// .env.example is committed and says your_key_here. Somebody who copied it
	// and never edited it has no key, and scheduled times only is a better answer
	// than asking the endpoint about a placeholder.
	isolated(t)
	t.Setenv("OC_TRANSPO_SUBSCRIPTION_KEY", placeholder)
	if got := Key(); got != "" {
		t.Errorf("Key = %q, want none: the template's stand-in is not a key", got)
	}
}

func TestAKeyWithSpacesAroundItIsStillThatKey(t *testing.T) {
	isolated(t)
	t.Setenv("OC_TRANSPO_SUBSCRIPTION_KEY", "  a-key\n")
	if got := Key(); got != "a-key" {
		t.Errorf("Key = %q, want a-key", got)
	}
}

func TestAKeyIsReadOutOfADotEnvFile(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, text, want string
	}{
		{"plain", "OCT_SUBSCRIPTION_KEY=a-key\n", "a-key"},
		{"quoted", `SUBSCRIPTION_KEY="a-key"` + "\n", "a-key"},
		{"single quoted", "SUBSCRIPTION_KEY='a-key'\n", "a-key"},
		{"spaced", "  SUBSCRIPTION_KEY = a-key  \n", "a-key"},
		{"after a comment", "# SUBSCRIPTION_KEY=not-this\nSUBSCRIPTION_KEY=a-key\n", "a-key"},
		{"beside other variables", "HOME=/tmp\nSUBSCRIPTION_KEY=a-key\n", "a-key"},
		{"a name nothing knows", "SOME_OTHER_KEY=a-key\n", ""},
		{"a line with no value", "SUBSCRIPTION_KEY\n", ""},
		{"the template, unedited", "OC_TRANSPO_SUBSCRIPTION_KEY=your_key_here\n", ""},
		{"nothing at all", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := keyIn(tc.text); got != tc.want {
				t.Errorf("keyIn(%q) = %q, want %q", tc.text, got, tc.want)
			}
		})
	}
}

func TestTheWorkingDirectorysFileWinsOverTheConfigDirectorys(t *testing.T) {
	// The config directory is where a key lives on a real machine. A file in the
	// directory the program was started in is somebody working on the program,
	// and that is the one they mean.
	dir := isolated(t)

	config := filepath.Join(dir, ".config", "otransit")
	if err := os.MkdirAll(config, 0o755); err != nil {
		t.Fatalf("making the config directory: %v", err)
	}
	if err := os.WriteFile(filepath.Join(config, ".env"), []byte("SUBSCRIPTION_KEY=from-the-config\n"), 0o600); err != nil {
		t.Fatalf("writing the config file: %v", err)
	}

	// The directory the test runs in has no .env of its own, so the config's key
	// is the one found.
	if got := Key(); got != "from-the-config" {
		t.Errorf("Key = %q, want the config directory's", got)
	}

	// And a file here wins.
	if err := os.WriteFile(filepath.Join(dir, ".env"), []byte("SUBSCRIPTION_KEY=from-here\n"), 0o600); err != nil {
		t.Fatalf("writing the local file: %v", err)
	}
	if got := Key(); got != "from-here" {
		t.Errorf("Key = %q, want the working directory's", got)
	}
}

func TestNoKeyAnywhereIsNotAnError(t *testing.T) {
	// A missing key means scheduled times only, and the status bar says so.
	isolated(t)

	if got := Key(); got != "" {
		t.Errorf("Key = %q, want none: a key printed here would be a real one", got)
	}
}

func TestAStatusTheFeedCallsAnErrorIsNamedAsOne(t *testing.T) {
	t.Parallel()
	// net/http returns no error for any status, so a feed that said no arrives
	// looking exactly like one that answered. A rejected key is worth its own
	// sentence: it is the one a person can do something about.
	for _, tc := range []struct {
		status int
		want   string
	}{
		{200, ""},
		{204, ""},
		{401, "rejected the key"},
		{403, "rejected the key"},
		{404, "answered 404"},
		{503, "answered 503"},
	} {
		err := refused(tc.status)
		switch {
		case tc.want == "" && err != nil:
			t.Errorf("refused(%d) = %v, want nothing", tc.status, err)
		case tc.want != "" && err == nil:
			t.Errorf("refused(%d) = nothing, want %q", tc.status, tc.want)
		case tc.want != "" && !strings.Contains(err.Error(), tc.want):
			t.Errorf("refused(%d) = %q, want it to say %q", tc.status, err, tc.want)
		}
	}
}

func TestEveryRefusalFitsTheStatusBar(t *testing.T) {
	t.Parallel()
	// A refusal is drawn beside the pin key and the two keys that leave, on one
	// row. net/http's own errors carry the method and the whole URL, which is 96
	// characters of compiled-in address that pushes `esc` and `q` off the screen
	// exactly when a person cannot see what is wrong and wants to leave.
	//
	// The text of each is asserted and not only its length: a rule that measured
	// them could not tell a timeout from any other silence, and which way it
	// failed is the part a person can act on.
	for _, tc := range []struct {
		name string
		err  error
		want string
	}{
		{"a rejected key", refused(401), "the feed rejected the key"},
		{"a forbidden key", refused(403), "the feed rejected the key"},
		{"a status", refused(503), "the feed answered 503"},
		{"no answer", brief(call(errors.New("dial tcp: lookup nextrip-public-api.azure-api.net: no such host"))), "the feed did not answer"},
		{"a timeout", brief(call(timedOut{})), "the feed timed out"},
		{"nobody waited", brief(call(context.Canceled)), "the feed was not waited for"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if tc.err == nil {
				t.Fatal("no refusal to measure")
			}
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("the refusal reads %q, want %q", got, tc.want)
			}
			if n := len([]rune(tc.err.Error())); n > bar {
				t.Errorf("%q is %d cells, and the bar has room for %d", tc.err, n, bar)
			}
		})
	}
}

// call is a transport failure as net/http reports one, with the whole URL in it.
func call(because error) error {
	return &url.Error{Op: "Get", URL: tripsURL + "?format=json", Err: because}
}

func TestAFetchThatCannotEvenStartIsBriefAboutIt(t *testing.T) {
	t.Parallel()
	// The classification has to be on the path a caller takes, and not only in a
	// function a test can reach. A context already cancelled fails the request
	// before a socket is opened, which is the shortest way through the real code.
	ctx, stop := context.WithCancel(context.Background())
	stop()

	_, err := Trips(ctx, "a-key")
	if err == nil {
		t.Fatal("a cancelled fetch worked")
	}
	if n := len([]rune(err.Error())); n > bar {
		t.Errorf("the refusal is %d cells: %q", n, err)
	}
	if strings.Contains(err.Error(), "http") {
		t.Errorf("%q carries the URL, which is compiled in and not news", err)
	}
}

// timedOut is what net/http's own timeout looks like to errors.As.
type timedOut struct{}

func (timedOut) Error() string { return "context deadline exceeded" }
func (timedOut) Timeout() bool { return true }

func TestAnErrorFromSomewhereElseIsLeftAlone(t *testing.T) {
	t.Parallel()
	// brief is about the transport. A feed that answered and then would not parse
	// has something specific to say, and the parser said it.
	own := errors.New("more than one feed in the body")
	if got := brief(own); got != own {
		t.Errorf("brief rewrote %q as %q", own, got)
	}
}

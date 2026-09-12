package script

import (
	"slices"
	"strings"
	"testing"
)

const world = `# a comment
cache ../cache.db
date 2026-09-12
time 08:00
offset -04:00
size 100 14

enter
type billings
`

func TestAScriptSetsItsWorldBeforeItsFirstStep(t *testing.T) {
	t.Parallel()
	got, err := Parse("script", strings.NewReader(world))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Cache != "../cache.db" || got.Date != "2026-09-12" || got.Time != "08:00" {
		t.Errorf("worldHeader = %q %q %q", got.Cache, got.Date, got.Time)
	}
	if got.Offset != -4*3600 {
		t.Errorf("Offset = %d, want %d", got.Offset, -4*3600)
	}
	if got.Width != 100 || got.Height != 14 {
		t.Errorf("size = %dx%d, want 100x14", got.Width, got.Height)
	}
	if len(got.Steps) != 2 {
		t.Fatalf("Steps = %d, want 2", len(got.Steps))
	}
}

func TestAStepCarriesTheSourceLineThatNamesItsFrame(t *testing.T) {
	t.Parallel()
	got, err := Parse("script", strings.NewReader(world))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := []string{"enter", "type billings"}
	for i, w := range want {
		if got.Steps[i].Label != w {
			t.Errorf("Steps[%d].Label = %q, want %q", i, got.Steps[i].Label, w)
		}
	}
}

func TestEveryStepFormParses(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		line string
		want Step
	}{
		{"up", "up", Step{Kind: Up, Label: "up"}},
		{"down", "down", Step{Kind: Down, Label: "down"}},
		{"enter", "enter", Step{Kind: Enter, Label: "enter"}},
		{"esc", "esc", Step{Kind: Esc, Label: "esc"}},
		{"backspace", "backspace", Step{Kind: Backspace, Label: "backspace"}},
		{"key", "key p", Step{Kind: Key, Text: "p", Label: "key p"}},
		{"type", "type billings", Step{Kind: Type, Text: "billings", Label: "type billings"}},
		{"wait", "wait 90", Step{Kind: Wait, Secs: 90, Label: "wait 90"}},
		{"wait as the fixtures write it", "wait 90s", Step{Kind: Wait, Secs: 90, Label: "wait 90s"}},
		{"a pole number keeps its hash", "type #3009", Step{Kind: Type, Text: "#3009", Label: "type #3009"}},
		{"typed text keeps its spaces", "type a b", Step{Kind: Type, Text: "a b", Label: "type a b"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse("script", strings.NewReader(worldHeader+tc.line+"\n"))
			if err != nil {
				t.Fatalf("Parse(%q): %v", tc.line, err)
			}
			if len(got.Steps) != 1 {
				t.Fatalf("Steps = %d, want 1", len(got.Steps))
			}
			if got.Steps[0] != tc.want {
				t.Errorf("Step = %+v, want %+v", got.Steps[0], tc.want)
			}
		})
	}
}

const worldHeader = "cache ../cache.db\ndate 2026-09-12\ntime 08:00\noffset -04:00\nsize 100 14\n"

func TestACommentIsIgnoredOutsideAType(t *testing.T) {
	t.Parallel()
	got, err := Parse("script", strings.NewReader(worldHeader+"# not a step\nenter # trailing\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(got.Steps) != 1 || got.Steps[0].Kind != Enter {
		t.Fatalf("Steps = %+v, want one enter", got.Steps)
	}
	if got.Steps[0].Label != "enter" {
		t.Errorf("Label = %q, want %q", got.Steps[0].Label, "enter")
	}
}

func TestEveryMalformedFormIsAnErrorThatNamesTheLine(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, src, want string
	}{
		// The directive must be missing from worldHeader here. With it present,
		// a duplicate-line error satisfies the assertion and the ordering rule
		// goes untested. That version of this case passed with the rule removed.
		{"a header line after a step", strings.Replace(worldHeader, "size 100 14\n", "", 1) + "enter\nsize 100 14\n", "belongs above the first step"},
		{"a second header line", worldHeader + "time 09:00\nenter\n", "a second time line"},
		{"an unknown verb", worldHeader + "jump\n", "line 6"},
		{"an offset with no sign", worldHeader[:strings.Index(worldHeader, "offset")] + "offset 04:00\nsize 100 14\n", "offset"},
		{"an offset with no colon", strings.Replace(worldHeader, "-04:00", "-0400", 1), "offset"},
		// Atoi accepted the second sign, so this parsed as four hours west.
		{"an offset with two signs", strings.Replace(worldHeader, "-04:00", "+-4:00", 1), "offset"},
		{"an offset past the widest zone", strings.Replace(worldHeader, "-04:00", "+99:00", 1), "offset"},
		{"an offset with letters", strings.Replace(worldHeader, "-04:00", "-ab:cd", 1), "offset"},
		{"a size with one number", strings.Replace(worldHeader, "size 100 14", "size 100", 1), "size"},
		{"a size that is not a number", strings.Replace(worldHeader, "size 100 14", "size wide 14", 1), "size"},
		{"a date that is not a date", strings.Replace(worldHeader, "2026-09-12", "2026-13-40", 1), "date"},
		{"a time that is not a time", strings.Replace(worldHeader, "08:00", "25:00", 1), "time"},
		{"wait with no number", worldHeader + "wait\n", "wait"},
		{"wait with a word", worldHeader + "wait soon\n", "wait"},
		{"key with no character", worldHeader + "key\n", "key"},
		{"key with two characters", worldHeader + "key pq\n", "key"},
		{"type with no text", worldHeader + "type\n", "type"},
		{"a missing cache", strings.Replace(worldHeader, "cache ../cache.db\n", "", 1), "cache"},
		{"a missing size", strings.Replace(worldHeader, "size 100 14\n", "", 1), "size"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := Parse("script", strings.NewReader(tc.src))
			if err == nil {
				t.Fatalf("Parse(%q) = nil error, want one", tc.src)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not mention %q", err, tc.want)
			}
		})
	}
}

func TestAnOffsetIsSecondsEastOfUTC(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, offset string
		want         int
	}{
		{"behind UTC", "-04:00", -4 * 3600},
		{"ahead of UTC", "+05:30", 5*3600 + 30*60},
		{"UTC itself", "+00:00", 0},
		{"negative half hour", "-09:30", -(9*3600 + 30*60)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Parse("script", strings.NewReader(strings.Replace(worldHeader, "-04:00", tc.offset, 1)))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Offset != tc.want {
				t.Errorf("Offset = %d, want %d", got.Offset, tc.want)
			}
		})
	}
}

func TestACachePathIsResolvedAgainstTheScriptItWasWrittenIn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, dir, cache, want string
	}{
		{"relative to the script", "conformance/empty", "../cache.db", "conformance/cache.db"},
		{"beside the script", "conformance/empty", "cache.db", "conformance/empty/cache.db"},
		{"absolute is already where it says", "conformance/empty", "/var/lib/cache.db", "/var/lib/cache.db"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s := &Script{Dir: tc.dir, Cache: tc.cache}
			if got := s.CachePath(); got != tc.want {
				t.Errorf("CachePath() = %q, want %q", got, tc.want)
			}
		})
	}
}

// The queue says what the endpoint will answer and never when the app will ask.
func TestTheFeedQueueIsReadInOrder(t *testing.T) {
	t.Parallel()
	s, err := Parse("script", strings.NewReader(worldHeader+
		"feed ok rt.json\nfeed fail http status: 503\nfeed ok rt-later.json\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(s.Steps) != 0 {
		t.Errorf("the queue made %d steps, want none: an answer is not a keypress", len(s.Steps))
	}
	want := []Answer{
		{File: "rt.json"},
		{Refusal: "http status: 503"},
		{File: "rt-later.json"},
	}
	if !slices.Equal(s.Answers, want) {
		t.Errorf("the queue is %+v, want %+v", s.Answers, want)
	}
}

// A refusal keeps its words, hash and all. They reach the status bar, where a
// person reads them, so nothing here edits them: a `#` in a refusal is not the
// start of a comment.
func TestARefusalKeepsEveryWordIncludingAHash(t *testing.T) {
	t.Parallel()
	s, err := Parse("script", strings.NewReader(worldHeader+"feed fail no route #7 today\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(s.Answers) != 1 || s.Answers[0].Refusal != "no route #7 today" {
		t.Errorf("the refusal is %+v, want every word of it", s.Answers)
	}
}

// An answer that says nothing is an error. A queue entry with no file and no
// words would be an attempt whose outcome the script never decided.
func TestAnAnswerMustSayWhatItIs(t *testing.T) {
	t.Parallel()
	for _, line := range []string{
		"feed\n",
		"feed ok\n",
		"feed fail\n",
		"feed maybe rt.json\n",
	} {
		t.Run(strings.TrimSpace(line), func(t *testing.T) {
			t.Parallel()
			if _, err := Parse("script", strings.NewReader(worldHeader+line)); err == nil {
				t.Errorf("Parse on %q = nil error, want one", strings.TrimSpace(line))
			}
		})
	}
}

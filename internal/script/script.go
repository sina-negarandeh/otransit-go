// Package script reads a conformance fixture script. A script names the world
// a replay runs in and the keys it presses over that world.
//
// The format is in conformance/README.md. This package is the only thing that
// knows it, and nothing here reads the machine.
package script

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Kind is what a step does.
type Kind int

// The steps a script may hold. Every one except Wait is a keypress.
const (
	Up Kind = iota
	Down
	Enter
	Esc
	Backspace
	Key
	Type
	Wait
)

// Step is one line of the script below the header.
type Step struct {
	Kind Kind
	// Text holds the character for Key and the string for Type.
	Text string
	// Secs holds the number of seconds for Wait.
	Secs int
	// Label is the source line. It names the frame the step produces.
	Label string
}

// Script is a fixture's world and the session recorded over it.
type Script struct {
	// Dir is the directory the script was read from. Load sets it and Parse
	// leaves it empty.
	Dir string
	// Cache is the cache path exactly as the script wrote it. CachePath
	// resolves it, and an error quotes this back, because this is what the
	// person who wrote the script will look for.
	Cache string
	// Date is the service date, written YYYY-MM-DD.
	Date string
	// Time is the moment the run starts at, written HH:MM.
	Time string
	// Offset is seconds east of UTC. It is negative west of it.
	Offset        int
	Width, Height int
	Steps         []Step
	// Answers is what the endpoint will say, in the order it will say it. They
	// are a queue and not steps: the script says what the feed answers and never
	// when the app asks, because when to ask is the app's own decision.
	Answers []Answer
}

// Answer is one thing the realtime endpoint will say.
type Answer struct {
	// File is the feed to serve, relative to the script's directory. It is
	// empty when the attempt is refused.
	File string
	// Refusal is what the endpoint says when it refuses.
	Refusal string
}

// Load reads the script at path and records the directory it came from.
func Load(path string) (*Script, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening script: %w", err)
	}
	defer f.Close() //nolint:errcheck // a read-only file, and the parse already succeeded or failed

	s, err := Parse(path, f)
	if err != nil {
		return nil, err
	}
	s.Dir = filepath.Dir(path)
	return s, nil
}

// CachePath is the cache path resolved against the script's directory. An
// absolute path is already where it says it is.
func (s *Script) CachePath() string {
	if filepath.IsAbs(s.Cache) {
		return s.Cache
	}
	return filepath.Join(s.Dir, s.Cache)
}

// Parse reads a script. The name appears in error messages.
func Parse(name string, r io.Reader) (*Script, error) {
	var (
		s    Script
		seen = map[string]bool{}
	)

	sc := bufio.NewScanner(r)
	for n := 1; sc.Scan(); n++ {
		verb, rest, ok := directive(sc.Text())
		if !ok {
			continue
		}
		where := fmt.Sprintf("%s: line %d", name, n)

		if header(verb) {
			if len(s.Steps) > 0 {
				return nil, fmt.Errorf("%s: %s belongs above the first step, and it sets the whole run", where, verb)
			}
			if seen[verb] {
				return nil, fmt.Errorf("%s: a second %s line", where, verb)
			}
			seen[verb] = true
			if err := s.setHeader(verb, rest); err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			continue
		}

		if verb == "feed" {
			answer, err := parseAnswer(rest)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", where, err)
			}
			s.Answers = append(s.Answers, answer)
			continue
		}

		step, err := parseStep(verb, rest)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", where, err)
		}
		s.Steps = append(s.Steps, step)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("reading %s: %w", name, err)
	}

	for _, want := range []string{"cache", "date", "time", "offset", "size"} {
		if !seen[want] {
			return nil, fmt.Errorf("%s: no %s line, and a script must set its whole world", name, want)
		}
	}
	return &s, nil
}

// directive splits a line into its verb and the rest. It reports false for a
// line that holds no directive, which is a blank line or a comment.
//
// A `#` starts a comment everywhere except inside a `type`, because a pole
// number is written `#3009` throughout this program.
func directive(line string) (verb, rest string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}
	verb, rest, _ = strings.Cut(line, " ")
	// A pole number is written #3009 in this program, so a typed string keeps
	// its hash. So does a refusal: the endpoint's words reach the status bar and
	// this program does not edit them.
	if verb != "type" && verb != "feed" {
		rest, _, _ = strings.Cut(rest, "#")
	}
	return verb, strings.TrimSpace(rest), true
}

func header(verb string) bool {
	switch verb {
	case "cache", "date", "time", "offset", "size":
		return true
	}
	return false
}

func (s *Script) setHeader(verb, rest string) error {
	switch verb {
	case "cache":
		if rest == "" {
			return fmt.Errorf("cache names no file")
		}
		s.Cache = rest
	case "date":
		if _, err := time.Parse(time.DateOnly, rest); err != nil {
			return fmt.Errorf("date %q is not a date written YYYY-MM-DD", rest)
		}
		s.Date = rest
	case "time":
		if _, err := time.Parse("15:04", rest); err != nil {
			return fmt.Errorf("time %q is not a time written HH:MM", rest)
		}
		s.Time = rest
	case "offset":
		off, err := offsetSeconds(rest)
		if err != nil {
			return err
		}
		s.Offset = off
	case "size":
		w, h, err := size(rest)
		if err != nil {
			return err
		}
		s.Width, s.Height = w, h
	}
	return nil
}

// offsetSeconds reads ±HH:MM, which is also how a half-hour zone is written.
func offsetSeconds(text string) (int, error) {
	bad := fmt.Errorf("offset %q is not written ±HH:MM", text)
	if len(text) != 6 || text[3] != ':' {
		return 0, bad
	}
	var sign int
	switch text[0] {
	case '+':
		sign = 1
	case '-':
		sign = -1
	default:
		return 0, bad
	}
	// Atoi would take a second sign here, so `+-4:00` parsed as four hours
	// west of UTC and built the wrong zone for every frame in the fixture.
	h, okh := twoDigits(text[1:3])
	m, okm := twoDigits(text[4:6])
	if !okh || !okm {
		return 0, bad
	}
	// The widest zones anyone keeps are UTC-12:00 and UTC+14:00.
	if h > 14 || m > 59 {
		return 0, fmt.Errorf("offset %q is outside ±14:00", text)
	}
	return sign * (h*3600 + m*60), nil
}

func twoDigits(s string) (int, bool) {
	if len(s) != 2 || s[0] < '0' || s[0] > '9' || s[1] < '0' || s[1] > '9' {
		return 0, false
	}
	return int(s[0]-'0')*10 + int(s[1]-'0'), true
}

// maxCells bounds a screen in either direction.
const maxCells = 10000

func size(text string) (w, h int, err error) {
	bad := fmt.Errorf("size %q is not two whole numbers", text)
	cols, rows, ok := strings.Cut(text, " ")
	if !ok {
		return 0, 0, bad
	}
	if w, err = strconv.Atoi(strings.TrimSpace(cols)); err != nil || w < 1 {
		return 0, 0, bad
	}
	if h, err = strconv.Atoi(strings.TrimSpace(rows)); err != nil || h < 1 {
		return 0, 0, bad
	}
	// A screen is drawn into a grid of width by height cells, so a mistyped
	// number is an allocation and not a diff. Bounded like wait is, and well
	// past any terminal: the widest fixture is a hundred cells.
	if w > maxCells || h > maxCells {
		return 0, 0, fmt.Errorf("size %q is larger than %d cells, which no terminal is", text, maxCells)
	}
	return w, h, nil
}

// parseAnswer reads one queued answer: `feed ok <file>` or `feed fail <what it
// said>`.
//
// A refusal keeps its words. They reach the status bar, where a person reads
// them, so this is not a flag that some failure happened.
func parseAnswer(rest string) (Answer, error) {
	kind, detail, _ := strings.Cut(rest, " ")
	detail = strings.TrimSpace(detail)
	switch kind {
	case "ok":
		if detail == "" {
			return Answer{}, fmt.Errorf("feed ok names no file")
		}
		return Answer{File: detail}, nil
	case "fail":
		if detail == "" {
			return Answer{}, fmt.Errorf("feed fail says nothing about why")
		}
		return Answer{Refusal: detail}, nil
	}
	return Answer{}, fmt.Errorf("feed answers ok or fail, and it got %q", kind)
}

func parseStep(verb, rest string) (Step, error) {
	label := verb
	if rest != "" {
		label = verb + " " + rest
	}
	step := Step{Label: label}

	switch verb {
	case "up":
		step.Kind = Up
	case "down":
		step.Kind = Down
	case "enter":
		step.Kind = Enter
	case "esc":
		step.Kind = Esc
	case "backspace":
		step.Kind = Backspace
	case "key":
		if len([]rune(rest)) != 1 {
			return Step{}, fmt.Errorf("key takes one character, and it got %q", rest)
		}
		step.Kind, step.Text = Key, rest
	case "type":
		if rest == "" {
			return Step{}, fmt.Errorf("type takes the text to type, and it got none")
		}
		step.Kind, step.Text = Type, rest
	case "wait":
		// The README writes this step `wait <secs>`, and every fixture writes
		// `wait 90s`. Both are accepted, and the label stays as it was written.
		secs, err := strconv.Atoi(strings.TrimSuffix(rest, "s"))
		if err != nil || secs < 0 {
			return Step{}, fmt.Errorf("wait takes a whole number of seconds, and it got %q", rest)
		}
		if secs > 86400 {
			return Step{}, fmt.Errorf("wait %d is longer than a day, which overflows the service day", secs)
		}
		step.Kind, step.Secs = Wait, secs
	default:
		return Step{}, fmt.Errorf("%q is not a step", verb)
	}

	if rest != "" && step.Kind != Key && step.Kind != Type && step.Kind != Wait {
		return Step{}, fmt.Errorf("%s takes nothing after it, and it got %q", verb, rest)
	}
	return step, nil
}

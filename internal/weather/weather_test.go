package weather

import (
	"strconv"
	"strings"
	"testing"
)

// feedWith builds the shape ECCC publishes, with the two fields that matter and
// the two that must be ignored.
func feedWith(condition string, code int, temp float64) string {
	return `{"id":"on-118","properties":{"currentConditions":{
		"condition":{"en":"` + condition + `","fr":"x"},
		"iconCode":{"format":"gif","value":` + itoa(code) + `,"url":"x"},
		"temperature":{"units":{"en":"C"},"qaValue":{"en":100},"value":{"en":` + ftoa(temp) + `,"fr":0}},
		"windChill":{"qaValue":{"en":100},"value":{"en":-2,"fr":-2}},
		"humidex":{"value":{"en":40,"fr":40}}}}}`
}

func itoa(n int) string { return strconv.Itoa(n) }

func ftoa(f float64) string { return strconv.FormatFloat(f, 'f', 1, 64) }

func TestTheLabelIsTheGlyphTheConditionAndTheTemperature(t *testing.T) {
	t.Parallel()
	got, err := Parse(strings.NewReader(feedWith("Mainly Cloudy", 32, 6.4)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if want := "☁ mainly cloudy · 6°"; got.Label() != want {
		t.Errorf("Label() = %q, want %q", got.Label(), want)
	}
}

// A code from 30 to 39 is the night form of the code ten times lower, and folds
// to it. Without the fold, 32 is a code nobody knows and draws nothing, which
// looks exactly like weather-unknown.
func TestANightCodeFoldsOntoItsDayForm(t *testing.T) {
	t.Parallel()
	for code := 30; code <= 39; code++ {
		night, err := Parse(strings.NewReader(feedWith("x", code, 0)))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		day, err := Parse(strings.NewReader(feedWith("x", code-30, 0)))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if night.Glyph != day.Glyph || night.Glyph == "" {
			t.Errorf("code %d draws %q and code %d draws %q", code, night.Glyph, code-30, day.Glyph)
		}
	}
}

// A code this program does not know draws no glyph. Not a stand-in: the label
// reads `condition · temp`, and a stand-in would sit beside the separator and
// read as a character of its own.
func TestAnUnknownCodeDrawsNoGlyphAtAll(t *testing.T) {
	t.Parallel()
	for _, code := range []int{21, 22, 29, 41, 42, 43, 48, 49, 93, 99, -1, 1000} {
		got, err := Parse(strings.NewReader(feedWith("Funnel Cloud", code, -12.4)))
		if err != nil {
			t.Fatalf("Parse: %v", err)
		}
		if got.Glyph != "" {
			t.Errorf("code %d draws %q, want nothing", code, got.Glyph)
		}
		if want := "funnel cloud · -12°"; got.Label() != want {
			t.Errorf("code %d gives %q, want %q", code, got.Label(), want)
		}
	}
}

// The temperature rounds away from zero, so a minus sign appears where the
// reading is below a half degree.
func TestTheTemperatureRoundsAwayFromZero(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		reading float64
		want    int
	}{
		{6.4, 6}, {6.5, 7}, {6.6, 7},
		{-0.4, 0}, {-0.5, -1}, {-0.6, -1},
		{20.7, 21}, {-12.5, -13}, {0, 0},
	} {
		t.Run(ftoa(tc.reading), func(t *testing.T) {
			t.Parallel()
			got, err := Parse(strings.NewReader(feedWith("x", 0, tc.reading)))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if got.Degrees != tc.want {
				t.Errorf("%v degrees became %d, want %d", tc.reading, got.Degrees, tc.want)
			}
		})
	}
}

// ECCC publishes windChill and humidex even when they make no sense. Ottawa
// served a wind chill of -2 at 20.7 degrees in light rain, flagged as good
// data. Neither field is read.
func TestTheWindChillAndHumidexAreNotRead(t *testing.T) {
	t.Parallel()
	got, err := Parse(strings.NewReader(feedWith("Light Rain", 12, 20.7)))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if got.Degrees != 21 {
		t.Errorf("Degrees = %d, want 21 from the temperature and not -2 from the wind chill", got.Degrees)
	}
}

// Every glyph is a single rune. That it is also a single CELL is measured in
// internal/render, over every literal this program can draw, because the glyph
// that breaks alignment is the one nobody thought to put on a list.
func TestEveryGlyphIsOneRune(t *testing.T) {
	t.Parallel()
	for code, glyph := range glyphs {
		if n := len([]rune(glyph)); n != 1 {
			t.Errorf("code %d draws %q, which is %d runes", code, glyph, n)
		}
	}
}

func TestAFeedThatIsNotTheFeedIsAnError(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, src string }{
		{"not json", "{"},
		{"no current conditions", `{"properties":{}}`},
		{"no temperature", `{"properties":{"currentConditions":{"condition":{"en":"x"}}}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := Parse(strings.NewReader(tc.src)); err == nil {
				t.Errorf("Parse(%q) = nil error, want one", tc.src)
			}
		})
	}
}

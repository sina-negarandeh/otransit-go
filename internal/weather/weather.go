// Package weather reads the current conditions from ECCC's citypage feed.
//
// ECCC labels that collection experimental, so its shape will change. Two
// fields are read and two are deliberately not. The picture comes from
// `iconCode` and never from the condition text, because the text is prose and
// the code is a number. `windChill` and `humidex` are published even when they
// are nonsense: Ottawa served a wind chill of -2 at 20.7 degrees in light rain,
// flagged as good data. Neither is read.
package weather

import (
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"
	"strings"
)

// Conditions is what the feed says the weather is now.
type Conditions struct {
	// Glyph is the one-cell picture for the icon code, and is empty for a code
	// this program does not know.
	Glyph string
	// Condition is the feed's own words, in lower case.
	Condition string
	// Degrees is the temperature, rounded away from zero.
	Degrees int
}

// glyphs is the picture for each icon code ECCC publishes.
//
// A code from 30 to 39 is the night form of the code thirty lower and folds
// onto it before this table is consulted. A code absent from it draws nothing.
//
// Every glyph here occupies one cell. ⛅ and ⚡ are East Asian Wide and would
// shift the whole label by a cell with nothing to say so, which is why the
// cloudy and the rainy codes draw ☁ and ⛆.
var glyphs = map[int]string{
	0: "☀", 1: "☀",
	2: "☁", 3: "☁", 4: "☁", 5: "☁", 10: "☁",
	6: "⛆", 7: "⛆", 9: "⛆", 11: "⛆", 12: "⛆", 13: "⛆", 14: "⛆", 15: "⛆",
	19: "⛆", 27: "⛆", 28: "⛆", 45: "⛆", 46: "⛆", 47: "⛆",
	8: "❄", 16: "❄", 17: "❄", 18: "❄", 25: "❄", 26: "❄", 40: "❄",
	20: "≈", 23: "≈", 24: "≈", 44: "≈",
}

// Parse reads the feed. A file that is present must parse: a fixture's whole
// claim is that the directory decides the output, so a feed this program cannot
// read is an error and not a quiet nothing.
func Parse(r io.Reader) (Conditions, error) {
	var feed struct {
		Properties struct {
			Current *struct {
				Condition struct {
					En string `json:"en"`
				} `json:"condition"`
				IconCode struct {
					Value *int `json:"value"`
				} `json:"iconCode"`
				Temperature struct {
					Value *struct {
						En *float64 `json:"en"`
					} `json:"value"`
				} `json:"temperature"`
			} `json:"currentConditions"`
		} `json:"properties"`
	}

	if err := json.NewDecoder(r).Decode(&feed); err != nil {
		return Conditions{}, fmt.Errorf("reading the weather feed: %w", err)
	}

	now := feed.Properties.Current
	if now == nil {
		return Conditions{}, fmt.Errorf("the weather feed carries no current conditions")
	}
	if now.Temperature.Value == nil || now.Temperature.Value.En == nil {
		return Conditions{}, fmt.Errorf("the weather feed carries no temperature")
	}

	c := Conditions{
		Condition: strings.ToLower(now.Condition.En),
		// Away from zero, so a reading of half a degree below freezing shows
		// the minus sign it earned.
		Degrees: int(math.Round(*now.Temperature.Value.En)),
	}
	if now.IconCode.Value != nil {
		c.Glyph = glyphs[fold(*now.IconCode.Value)]
	}
	return c, nil
}

// fold turns a night code into its day form. ECCC publishes 30 to 39 as the
// night versions of 0 to 9, and a sweep of the live feed confirmed the pairing.
func fold(code int) int {
	if code >= 30 && code <= 39 {
		return code - 30
	}
	return code
}

// Label is the line the rule carries: the glyph, the condition and the
// temperature. A code with no glyph leaves the space out as well, because the
// label would otherwise begin with one.
func (c Conditions) Label() string {
	var b strings.Builder
	if c.Glyph != "" {
		b.WriteString(c.Glyph + " ")
	}
	b.WriteString(c.Condition + " · " + strconv.Itoa(c.Degrees) + "°")
	return b.String()
}

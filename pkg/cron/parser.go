// Copyright © 2026 Beijing Ruishuo Technology Co., Ltd.
// SPDX-License-Identifier: AGPL-3.0-or-later
// Dual-licensed — see LICENSE for details.

package cron

import (
	"fmt"
	"math/bits"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ParseOption is a bit flag of options to control how the parser interprets
// cron expression fields.
type ParseOption int

// Constants for the cron parser option bit flags, each enabling an optional
// or required cron expression field.
const (
	Second         ParseOption = 1 << iota // Seconds field, default 0
	SecondOptional                         // Optional seconds field, default 0
	Minute                                 // Minutes field, default 0
	Hour                                   // Hours field, default 0
	Dom                                    // Day of month field, default *
	Month                                  // Month field, default *
	Dow                                    // Day of week field, default *
	DowOptional                            // Optional day of week field, default *
	Descriptor                             // Allow @every, @hourly etc descriptors
)

var monthNames = map[string]int{
	"JAN": 1, "FEB": 2, "MAR": 3, "APR": 4,
	"MAY": 5, "JUN": 6, "JUL": 7, "AUG": 8,
	"SEP": 9, "OCT": 10, "NOV": 11, "DEC": 12,
}

var weekdayNames = map[string]int{
	"SUN": 0, "MON": 1, "TUE": 2, "WED": 3,
	"THU": 4, "FRI": 5, "SAT": 6,
}

// fieldBounds defines supported cron field ranges.
var fieldBounds = []struct {
	min int
	max int
}{
	{0, 59}, // seconds
	{0, 59}, // minutes
	{0, 23}, // hours
	{1, 31}, // day of month
	{1, 12}, // month
	{0, 7},  // day of week (0 and 7 both represent Sunday)
}

// fieldPlaces lists the spec field parse options in canonical order,
// matching the field order in fieldBounds.
var fieldPlaces = []ParseOption{Second, Minute, Hour, Dom, Month, Dow}

// fieldDefaults lists the default token applied to each field when the
// field is absent or optional.
var fieldDefaults = []string{"0", "0", "0", "*", "*", "*"}

// Parser is a configurable cron expression parser.
type Parser struct {
	options ParseOption
}

// NewParser creates a Parser with the given options.
// Returns an error if more than one optional flag is configured.
func NewParser(options ParseOption) (Parser, error) {
	optionals := 0
	if options&DowOptional > 0 {
		optionals++
	}
	if options&SecondOptional > 0 {
		optionals++
	}
	if optionals > 1 {
		return Parser{}, fmt.Errorf("cron: multiple optional flags may not be configured")
	}
	return Parser{options: options}, nil
}

// defaultParser fields are lazily initialized via [sync.Once] to avoid a
// package-level init panic. The default config is a compile-time constant
// known to be valid, so the error path is unreachable in practice; the
// error is propagated through [Parse] rather than panicking to honor the
// "no panic in business logic" rule.
var (
	defaultParserOnce sync.Once
	defaultParser     Parser
	defaultParserErr  error
)

// initDefaultParser lazily initializes and returns the default parser.
// The error is unreachable in practice (the default config is valid),
// but is returned rather than panicking to honor the "no panic in
// business logic" rule.
func initDefaultParser() (Parser, error) {
	defaultParserOnce.Do(func() {
		defaultParser, defaultParserErr = NewParser(
			SecondOptional | Minute | Hour | Dom | Month | Dow | Descriptor,
		)
	})
	return defaultParser, defaultParserErr
}

// Parse parses a cron expression using the default parser.
func Parse(expr string) (Schedule, error) {
	p, err := initDefaultParser()
	if err != nil {
		return nil, fmt.Errorf("cron: init default parser: %w", err)
	}
	return p.Parse(expr)
}

// Parse parses a cron expression according to the parser's options.
func (p Parser) Parse(spec string) (Schedule, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return nil, fmt.Errorf("cron: empty spec string")
	}

	// Extract timezone prefix
	spec, loc, err := extractTimezone(spec)
	if err != nil {
		return nil, err
	}

	// Handle @ descriptors
	if strings.HasPrefix(spec, "@") {
		if p.options&Descriptor == 0 {
			return nil, fmt.Errorf("cron: descriptors are not supported")
		}
		return parseDescriptor(spec, loc)
	}

	// Split spec into fields
	fields := strings.Fields(spec)

	// Calculate expected field count
	minFields, maxFields := p.fieldCount()
	if len(fields) < minFields || len(fields) > maxFields {
		return nil, fmt.Errorf("cron: expected %d to %d fields, got %d", minFields, maxFields, len(fields))
	}

	sched, err := p.parseSpecFields(fields, loc)
	if err != nil {
		return nil, err
	}
	return sched, nil
}

// extractTimezone strips an optional "TZ=" or "CRON_TZ=" prefix from
// spec and returns the remaining spec together with the parsed
// location. When no prefix is present the spec is returned unchanged
// with time.Local.
func extractTimezone(spec string) (string, *time.Location, error) {
	loc := time.Local
	if !strings.HasPrefix(spec, "TZ=") && !strings.HasPrefix(spec, "CRON_TZ=") {
		return spec, loc, nil
	}
	parts := strings.SplitN(spec, " ", 2)
	var tzStr string
	if strings.HasPrefix(parts[0], "TZ=") {
		tzStr = strings.TrimPrefix(parts[0], "TZ=")
	} else {
		tzStr = strings.TrimPrefix(parts[0], "CRON_TZ=")
	}
	loc, err := time.LoadLocation(tzStr)
	if err != nil {
		return "", nil, fmt.Errorf("cron: provided bad location %s: %w", tzStr, err)
	}
	if len(parts) > 1 {
		return parts[1], loc, nil
	}
	return "", loc, nil
}

// fieldCount returns the minimum and maximum number of spec fields
// accepted under the parser's options.
func (p Parser) fieldCount() (minCount, maxCount int) {
	for _, place := range fieldPlaces {
		if p.options&place > 0 {
			minCount++
			maxCount++
		}
	}
	if p.options&SecondOptional > 0 {
		maxCount++
	}
	if p.options&DowOptional > 0 {
		maxCount++
	}
	return minCount, maxCount
}

// parseSpecFields parses each spec field into the schedule bitmasks and
// validates the resulting schedule.
func (p Parser) parseSpecFields(fields []string, loc *time.Location) (*specSchedule, error) {
	minFields, _ := p.fieldCount()
	sched := &specSchedule{loc: loc}
	extraFields := len(fields) - minFields
	domIsQuestion := false
	dowIsQuestion := false

	fieldIndex := 0
	for i, place := range fieldPlaces {
		token, nextFieldIndex, nextExtraFields := p.pickToken(i, place, fields, fieldIndex, extraFields)
		fieldIndex = nextFieldIndex
		extraFields = nextExtraFields

		fieldDomQuestion, fieldDowQuestion, err := checkToken(token, i)
		if err != nil {
			return nil, err
		}
		domIsQuestion = domIsQuestion || fieldDomQuestion
		dowIsQuestion = dowIsQuestion || fieldDowQuestion

		mask, err := parseField(token, fieldBounds[i].min, fieldBounds[i].max, i)
		if err != nil {
			return nil, err
		}

		applyFieldMask(sched, token, i, mask)
	}

	// DOM and DOW cannot both be ?
	if domIsQuestion && dowIsQuestion {
		return nil, fmt.Errorf("cron: ? cannot be used for both day-of-month and day-of-week")
	}

	// Validate non-zero fields
	if err := validateScheduleMasks(sched); err != nil {
		return nil, err
	}

	return sched, nil
}

// pickToken selects the raw spec token for the field at index i and
// returns the token together with the updated field index and remaining
// optional-field count.
func (p Parser) pickToken(
	i int,
	place ParseOption,
	fields []string,
	fieldIndex int,
	extraFields int,
) (token string, nextFieldIndex, nextExtraFields int) {
	switch {
	case p.options&place > 0:
		return fields[fieldIndex], fieldIndex + 1, extraFields
	case (place == Second && p.options&SecondOptional > 0) ||
		(place == Dow && p.options&DowOptional > 0):
		if extraFields > 0 {
			return fields[fieldIndex], fieldIndex + 1, extraFields - 1
		}
		return fieldDefaults[i], fieldIndex, extraFields
	default:
		return fieldDefaults[i], fieldIndex, extraFields
	}
}

// checkToken validates a single field token and reports whether the
// day-of-month (index 3) or day-of-week (index 5) token is "?".
func checkToken(token string, i int) (domIsQuestion, dowIsQuestion bool, err error) {
	// Reject L, W, # characters (case insensitive)
	upper := strings.ToUpper(token)
	if strings.ContainsAny(upper, "LW#") {
		return false, false, fmt.Errorf("cron: unsupported syntax: %s contains L, W, or #", token)
	}

	// ? only allowed in DOM (index 3) and DOW (index 5)
	if token == "?" && i != 3 && i != 5 {
		return false, false, fmt.Errorf("cron: ? is only allowed in day-of-month and day-of-week fields")
	}

	// Track ? for mutual exclusion check
	if token == "?" {
		if i == 3 {
			domIsQuestion = true
		}
		if i == 5 {
			dowIsQuestion = true
		}
	}
	return domIsQuestion, dowIsQuestion, nil
}

// applyFieldMask stores the parsed bitmask for the field at index i and
// marks the day-of-month/day-of-week star flags for "*" or "?" tokens.
func applyFieldMask(sched *specSchedule, token string, i int, mask uint64) {
	switch i {
	case 0:
		sched.sec = mask
	case 1:
		sched.min = mask
	case 2:
		sched.hour = mask
	case 3:
		sched.dom = mask
		if token == "*" || token == "?" {
			sched.domStar = true
		}
	case 4:
		sched.month = mask
	case 5:
		sched.dow = mask
		if token == "*" || token == "?" {
			sched.dowStar = true
		}
	}
}

// validateScheduleMasks rejects schedules in which any parsed field
// mask is empty.
func validateScheduleMasks(sched *specSchedule) error {
	if sched.sec == 0 {
		return fmt.Errorf("cron: seconds field cannot be empty")
	}
	if sched.min == 0 {
		return fmt.Errorf("cron: minutes field cannot be empty")
	}
	if sched.hour == 0 {
		return fmt.Errorf("cron: hours field cannot be empty")
	}
	if sched.dom == 0 {
		return fmt.Errorf("cron: day-of-month field cannot be empty")
	}
	if sched.month == 0 {
		return fmt.Errorf("cron: month field cannot be empty")
	}
	if sched.dow == 0 {
		return fmt.Errorf("cron: day-of-week field cannot be empty")
	}
	return nil
}

func parseDescriptor(spec string, loc *time.Location) (Schedule, error) {
	lower := strings.ToLower(spec)
	switch {
	case strings.HasPrefix(lower, "@every "):
		duration, err := time.ParseDuration(spec[7:])
		if err != nil {
			return nil, fmt.Errorf("cron: failed to parse duration %s: %w", spec[7:], err)
		}
		return Every(duration), nil

	case lower == "@yearly" || lower == "@annually":
		return &specSchedule{
			sec:     1 << 0,
			min:     1 << 0,
			hour:    1 << 0,
			dom:     1 << 1,
			month:   1 << 1,
			dow:     allBits(0, 7),
			dowStar: true,
			loc:     loc,
		}, nil

	case lower == "@monthly":
		return &specSchedule{
			sec:     1 << 0,
			min:     1 << 0,
			hour:    1 << 0,
			dom:     1 << 1,
			month:   allBits(1, 12),
			dow:     allBits(0, 7),
			dowStar: true,
			loc:     loc,
		}, nil

	case lower == "@weekly":
		return &specSchedule{
			sec:     1 << 0,
			min:     1 << 0,
			hour:    1 << 0,
			dom:     allBits(1, 31),
			month:   allBits(1, 12),
			dow:     1 << 0,
			domStar: true,
			loc:     loc,
		}, nil

	case lower == "@daily" || lower == "@midnight":
		return &specSchedule{
			sec:     1 << 0,
			min:     1 << 0,
			hour:    1 << 0,
			dom:     allBits(1, 31),
			month:   allBits(1, 12),
			dow:     allBits(0, 7),
			domStar: true,
			dowStar: true,
			loc:     loc,
		}, nil

	case lower == "@hourly":
		return &specSchedule{
			sec:     1 << 0,
			min:     1 << 0,
			hour:    allBits(0, 23),
			dom:     allBits(1, 31),
			month:   allBits(1, 12),
			dow:     allBits(0, 7),
			domStar: true,
			dowStar: true,
			loc:     loc,
		}, nil

	case lower == "@reboot":
		return &immediateSchedule{}, nil

	default:
		return nil, fmt.Errorf("cron: unrecognized descriptor: %s", spec)
	}
}

func parseField(token string, minVal, maxVal, index int) (uint64, error) {
	token = strings.TrimSpace(token)
	if token == "*" || token == "?" {
		return allBits(minVal, maxVal), nil
	}

	mask := uint64(0)
	for _, part := range strings.Split(token, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			return 0, fmt.Errorf("cron: invalid empty token")
		}

		rangePart, step, hasStep, err := parseStep(part)
		if err != nil {
			return 0, err
		}

		start, end, wrapped, err := parseBounds(rangePart, minVal, maxVal, index, hasStep)
		if err != nil {
			return 0, err
		}

		if wrapped {
			// Day-of-week wrap-around range (e.g. FRI-MON): fill from
			// start to maxVal and from minVal to end as two segments.
			mask = fillRange(mask, start, maxVal, step)
			mask = fillRange(mask, minVal, end, step)
			continue
		}

		mask = fillRange(mask, start, end, step)
	}

	return mask, nil
}

// parseStep splits a "value/step" token into its range part and step.
// It returns the range part (with an empty left side normalized to "*"),
// the step value, and whether a step was present.
func parseStep(part string) (rangePart string, step int, hasStep bool, err error) {
	if !strings.Contains(part, "/") {
		return part, 1, false, nil
	}
	pieces := strings.Split(part, "/")
	if len(pieces) != 2 {
		return "", 0, false, fmt.Errorf("cron: invalid step syntax: %s", part)
	}
	rangePart = strings.TrimSpace(pieces[0])
	if rangePart == "" {
		rangePart = "*"
	}
	stepValue, convErr := strconv.Atoi(pieces[1])
	if convErr != nil || stepValue <= 0 {
		return "", 0, false, fmt.Errorf("cron: invalid step value: %s", pieces[1])
	}
	return rangePart, stepValue, true, nil
}

// parseBounds resolves a single comma-separated part into its inclusive
// value range. For the day-of-week wrap-around range (start > end, e.g.
// FRI-MON) it returns wrapped=true; the caller must fill that range as
// two segments. When hasStep is set a single value extends to maxVal.
func parseBounds(
	part string,
	minVal, maxVal, index int,
	hasStep bool,
) (start, end int, wrapped bool, err error) {
	switch {
	case part == "*" || part == "?":
		return minVal, maxVal, false, nil
	case strings.Contains(part, "-"):
		rangePieces := strings.Split(part, "-")
		if len(rangePieces) != 2 {
			return 0, 0, false, fmt.Errorf("cron: invalid range syntax: %s", part)
		}
		var rangeErr error
		start, rangeErr = parseValue(rangePieces[0], minVal, maxVal, index)
		if rangeErr != nil {
			return 0, 0, false, rangeErr
		}
		end, rangeErr = parseValue(rangePieces[1], minVal, maxVal, index)
		if rangeErr != nil {
			return 0, 0, false, rangeErr
		}
		if start > end {
			if index == 5 {
				return start, end, true, nil
			}
			return 0, 0, false, fmt.Errorf("cron: range start cannot be greater than end: %s", part)
		}
		return start, end, false, nil
	default:
		value, valueErr := parseValue(part, minVal, maxVal, index)
		if valueErr != nil {
			return 0, 0, false, valueErr
		}
		if hasStep {
			return value, maxVal, false, nil
		}
		return value, value, false, nil
	}
}

// fillRange sets the bits for every value in [start, end] with the
// given step and returns the updated mask.
func fillRange(mask uint64, start, end, step int) uint64 {
	for value := start; value <= end; value += step {
		mask |= bitFor(value)
	}
	return mask
}

func parseValue(token string, minVal, maxVal, index int) (int, error) {
	token = strings.TrimSpace(strings.ToUpper(token))
	if token == "*" || token == "?" {
		return minVal, nil
	}

	if index == 4 {
		if v, ok := monthNames[token]; ok {
			return v, nil
		}
	}
	if index == 5 {
		if v, ok := weekdayNames[token]; ok {
			return v, nil
		}
		if token == "7" {
			return 0, nil
		}
	}

	value, err := strconv.Atoi(token)
	if err != nil {
		return 0, fmt.Errorf("cron: invalid numeric value %q", token)
	}

	if index == 5 && value == 7 {
		return 0, nil
	}

	if value < minVal || value > maxVal {
		return 0, fmt.Errorf("cron: value %d out of range for field %d", value, index)
	}
	return value, nil
}

func allBits(minVal, maxVal int) uint64 {
	if minVal > maxVal || maxVal-minVal+1 >= 64 {
		return ^uint64(0)
	}
	return ((uint64(1) << uint(maxVal-minVal+1)) - 1) << uint(minVal)
}

func bitFor(value int) uint64 {
	return uint64(1) << uint(value)
}

func bitMatch(mask uint64, value int) bool {
	return mask&(uint64(1)<<uint(value)) != 0
}

func nextSetBit(mask uint64, from, maxVal int) (int, bool) {
	if from > maxVal {
		return 0, false
	}
	shifted := mask >> uint(from)
	if shifted == 0 {
		return 0, false
	}
	pos := bits.TrailingZeros64(shifted)
	value := from + pos
	if value > maxVal {
		return 0, false
	}
	return value, true
}

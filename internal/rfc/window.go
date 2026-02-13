package rfc

import (
	"fmt"
	"strings"
	"time"

	"github.com/justinholmes/grunter/internal/config"
)

var dayMap = map[string]time.Weekday{
	"sunday":    time.Sunday,
	"monday":    time.Monday,
	"tuesday":   time.Tuesday,
	"wednesday": time.Wednesday,
	"thursday":  time.Thursday,
	"friday":    time.Friday,
	"saturday":  time.Saturday,
}

// CheckDeploymentWindow checks whether the given time falls within the
// configured deployment window. Returns a WindowResult indicating whether
// deployment is allowed, and if not, when the next window opens.
func CheckDeploymentWindow(w *config.DeploymentWindow, now time.Time) (*WindowResult, error) {
	loc, err := time.LoadLocation(w.Timezone)
	if err != nil {
		return nil, fmt.Errorf("loading timezone %q: %w", w.Timezone, err)
	}

	localNow := now.In(loc)

	startH, startM, err := parseHHMM(w.Start)
	if err != nil {
		return nil, fmt.Errorf("parsing start time: %w", err)
	}
	endH, endM, err := parseHHMM(w.End)
	if err != nil {
		return nil, fmt.Errorf("parsing end time: %w", err)
	}

	allowedDays := make(map[time.Weekday]bool)
	for _, d := range w.Days {
		wd, ok := dayMap[strings.ToLower(d)]
		if !ok {
			return nil, fmt.Errorf("invalid day %q", d)
		}
		allowedDays[wd] = true
	}

	// Check if current day is allowed
	if !allowedDays[localNow.Weekday()] {
		next := nextWindowStart(localNow, allowedDays, startH, startM)
		return &WindowResult{
			Allowed:         false,
			Message:         fmt.Sprintf("deployment not allowed on %s", localNow.Weekday()),
			NextWindowStart: &next,
		}, nil
	}

	// Check if current time is within the window
	y, m, d := localNow.Date()
	windowStart := time.Date(y, m, d, startH, startM, 0, 0, loc)
	windowEnd := time.Date(y, m, d, endH, endM, 0, 0, loc)

	if localNow.Before(windowStart) {
		return &WindowResult{
			Allowed:         false,
			Message:         fmt.Sprintf("deployment window opens at %s %s", w.Start, w.Timezone),
			NextWindowStart: &windowStart,
		}, nil
	}

	if localNow.After(windowEnd) || localNow.Equal(windowEnd) {
		next := nextWindowStart(localNow, allowedDays, startH, startM)
		return &WindowResult{
			Allowed:         false,
			Message:         fmt.Sprintf("deployment window closed at %s %s", w.End, w.Timezone),
			NextWindowStart: &next,
		}, nil
	}

	return &WindowResult{
		Allowed: true,
		Message: "within deployment window",
	}, nil
}

func parseHHMM(s string) (int, int, error) {
	var h, m int
	_, err := fmt.Sscanf(s, "%d:%d", &h, &m)
	if err != nil {
		return 0, 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("time out of range: %s", s)
	}
	return h, m, nil
}

// nextWindowStart finds the next time the deployment window opens,
// starting from the day after the given time.
func nextWindowStart(now time.Time, allowedDays map[time.Weekday]bool, startH, startM int) time.Time {
	loc := now.Location()
	candidate := now.AddDate(0, 0, 1)
	for i := 0; i < 7; i++ {
		if allowedDays[candidate.Weekday()] {
			y, m, d := candidate.Date()
			return time.Date(y, m, d, startH, startM, 0, 0, loc)
		}
		candidate = candidate.AddDate(0, 0, 1)
	}
	// Shouldn't happen if allowedDays is non-empty, but return next day as fallback.
	y, m, d := now.AddDate(0, 0, 1).Date()
	return time.Date(y, m, d, startH, startM, 0, 0, loc)
}

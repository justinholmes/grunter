package rfc

import (
	"testing"
	"time"

	"github.com/justinholmes/grunter/internal/config"
)

func TestCheckDeploymentWindow(t *testing.T) {
	weekdayWindow := &config.DeploymentWindow{
		Days:     []string{"monday", "tuesday", "wednesday", "thursday", "friday"},
		Start:    "09:00",
		End:      "17:00",
		Timezone: "UTC",
	}

	tests := []struct {
		name    string
		window  *config.DeploymentWindow
		now     time.Time
		allowed bool
		desc    string
	}{
		{
			name:    "within window",
			window:  weekdayWindow,
			now:     time.Date(2025, 1, 6, 12, 0, 0, 0, time.UTC), // Monday noon
			allowed: true,
		},
		{
			name:    "at window start",
			window:  weekdayWindow,
			now:     time.Date(2025, 1, 6, 9, 0, 0, 0, time.UTC), // Monday 09:00
			allowed: true,
		},
		{
			name:    "at window end",
			window:  weekdayWindow,
			now:     time.Date(2025, 1, 6, 17, 0, 0, 0, time.UTC), // Monday 17:00 (end boundary)
			allowed: false,
		},
		{
			name:    "before window start",
			window:  weekdayWindow,
			now:     time.Date(2025, 1, 6, 8, 30, 0, 0, time.UTC), // Monday 08:30
			allowed: false,
		},
		{
			name:    "after window end",
			window:  weekdayWindow,
			now:     time.Date(2025, 1, 6, 18, 0, 0, 0, time.UTC), // Monday 18:00
			allowed: false,
		},
		{
			name:    "saturday blocked",
			window:  weekdayWindow,
			now:     time.Date(2025, 1, 4, 12, 0, 0, 0, time.UTC), // Saturday noon
			allowed: false,
		},
		{
			name:    "sunday blocked",
			window:  weekdayWindow,
			now:     time.Date(2025, 1, 5, 12, 0, 0, 0, time.UTC), // Sunday noon
			allowed: false,
		},
		{
			name: "different timezone - within window",
			window: &config.DeploymentWindow{
				Days:     []string{"monday"},
				Start:    "09:00",
				End:      "17:00",
				Timezone: "America/New_York",
			},
			// 14:00 UTC = 09:00 EST on Monday Jan 6 2025
			now:     time.Date(2025, 1, 6, 14, 0, 0, 0, time.UTC),
			allowed: true,
		},
		{
			name: "different timezone - outside window",
			window: &config.DeploymentWindow{
				Days:     []string{"monday"},
				Start:    "09:00",
				End:      "17:00",
				Timezone: "America/New_York",
			},
			// 13:00 UTC = 08:00 EST on Monday Jan 6 2025 (before window)
			now:     time.Date(2025, 1, 6, 13, 0, 0, 0, time.UTC),
			allowed: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := CheckDeploymentWindow(tt.window, tt.now)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if result.Allowed != tt.allowed {
				t.Errorf("expected allowed=%v, got allowed=%v (message: %s)", tt.allowed, result.Allowed, result.Message)
			}
			if !result.Allowed && result.NextWindowStart == nil {
				t.Error("expected NextWindowStart to be set when not allowed")
			}
		})
	}
}

func TestCheckDeploymentWindow_NextWindowStart(t *testing.T) {
	window := &config.DeploymentWindow{
		Days:     []string{"monday", "wednesday", "friday"},
		Start:    "10:00",
		End:      "16:00",
		Timezone: "UTC",
	}

	// Tuesday noon - next allowed day is Wednesday
	now := time.Date(2025, 1, 7, 12, 0, 0, 0, time.UTC) // Tuesday
	result, err := CheckDeploymentWindow(window, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected not allowed on Tuesday")
	}

	expected := time.Date(2025, 1, 8, 10, 0, 0, 0, time.UTC) // Wednesday 10:00
	if !result.NextWindowStart.Equal(expected) {
		t.Errorf("expected next window %v, got %v", expected, *result.NextWindowStart)
	}
}

func TestCheckDeploymentWindow_AfterHours_NextDay(t *testing.T) {
	window := &config.DeploymentWindow{
		Days:     []string{"monday", "tuesday"},
		Start:    "09:00",
		End:      "17:00",
		Timezone: "UTC",
	}

	// Monday 18:00 - after hours, next allowed is Tuesday 09:00
	now := time.Date(2025, 1, 6, 18, 0, 0, 0, time.UTC) // Monday after hours
	result, err := CheckDeploymentWindow(window, now)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Allowed {
		t.Fatal("expected not allowed after hours")
	}

	expected := time.Date(2025, 1, 7, 9, 0, 0, 0, time.UTC) // Tuesday 09:00
	if !result.NextWindowStart.Equal(expected) {
		t.Errorf("expected next window %v, got %v", expected, *result.NextWindowStart)
	}
}

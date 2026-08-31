package backup_test

import (
	"strings"
	"testing"
	"time"

	"github.com/fusuycorp/pikpik/pkg/backup"
)

func TestAdversarial_Cron_OutOfRangeFieldValues(t *testing.T) {
	// CRON-01: Out-of-range field values
	outOfRangeExpressions := []string{
		"60 * * * *",       // Minute 60 (max 59)
		"-1 * * * *",       // Negative minute
		"* 24 * * *",       // Hour 24 (max 23)
		"* -5 * * *",       // Negative hour
		"* * 0 * *",        // Day 0 (min 1)
		"* * 32 * *",       // Day 32 (max 31)
		"* * * 0 *",        // Month 0 (min 1)
		"* * * 13 *",       // Month 13 (max 12)
		"* * * * 8",        // Weekday 8 (max 7)
		"* * * * -1",       // Negative weekday
		"0 0 0-35 * *",     // Day range exceeding 31
		"0 0 * 1-15 *",     // Month range exceeding 12
	}

	for _, expr := range outOfRangeExpressions {
		t.Run(expr, func(t *testing.T) {
			cron, err := backup.ParseCron(expr)
			if err == nil {
				t.Fatalf("CRON-01: expected error for out-of-range expression %q, got cron: %v", expr, cron)
			}
		})
	}
}

func TestAdversarial_Cron_DivisionByZeroStepValues(t *testing.T) {
	// CRON-02: Division by zero step values & negative steps
	zeroStepExpressions := []string{
		"*/0 * * * *",
		"0-30/0 * * * *",
		"* */0 * * *",
		"* * */0 * *",
		"* * * */0 *",
		"* * * * */0",
		"5-10/-2 * * * *",
		"*/-5 * * * *",
		"*/abc * * * *",
		"*/ * * * *",
	}

	for _, expr := range zeroStepExpressions {
		t.Run(expr, func(t *testing.T) {
			_, err := backup.ParseCron(expr)
			if err == nil {
				t.Fatalf("CRON-02: expected error for zero/invalid step %q, got nil", expr)
			}
		})
	}
}

func TestAdversarial_Cron_ImpossibleCalendarDates(t *testing.T) {
	// CRON-03: Inverted ranges
	invertedRanges := []string{
		"10-5 * * * *",
		"30-10/2 * * * *",
		"* 20-10 * * *",
		"* * 25-10 * *",
		"* * * 10-5 *",
	}

	for _, expr := range invertedRanges {
		t.Run("inverted_"+expr, func(t *testing.T) {
			_, err := backup.ParseCron(expr)
			if err == nil {
				t.Fatalf("CRON-03: expected error for inverted range %q, got nil", expr)
			}
		})
	}

	// CRON-04: Non-existent calendar date (Feb 30) evaluated over 5-year window
	feb30Cron, err := backup.ParseCron("0 0 30 2 *")
	if err != nil {
		t.Fatalf("unexpected error parsing valid syntax date: %v", err)
	}

	fromTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	done := make(chan time.Time)
	go func() {
		next := feb30Cron.Next(fromTime)
		done <- next
	}()

	select {
	case next := <-done:
		// Feb 30 never exists, so Next() must return zero time or terminate after 5 years
		if !next.IsZero() {
			t.Fatalf("CRON-04: expected zero time for impossible calendar date Feb 30, got %v", next)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CRON-04: Next() hung indefinitely in infinite loop searching for Feb 30!")
	}
}

func TestAdversarial_Cron_HostileStringsAndBufferStress(t *testing.T) {
	// CRON-05: Hostile field counts and massive strings
	hostileStrings := []struct {
		name string
		expr string
	}{
		{"Empty string", ""},
		{"Single field", "*"},
		{"Two fields", "* *"},
		{"Three fields", "* * *"},
		{"Four fields", "* * * *"},
		{"Six fields", "* * * * * *"},
		{"Hundred fields", strings.Repeat("* ", 100)},
		{"Massive 100KB string", strings.Repeat("a", 100*1024)},
		{"Null bytes in cron", "0 0 * \x00 *"},
		{"Special symbols", "@!#$%^&*()"},
		{"Double slash in step", "*/2/2 * * * *"},
		{"Multiple commas empty", "1,,2 * * * *"},
	}

	for _, tc := range hostileStrings {
		t.Run(tc.name, func(t *testing.T) {
			_, err := backup.ParseCron(tc.expr)
			if err == nil {
				t.Fatalf("CRON-05: expected error for hostile string %s, got nil", tc.name)
			}
		})
	}
}

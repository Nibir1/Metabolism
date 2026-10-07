package main

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Every test in this package runs with an off-peak clock, so none of them
// depends on the time of day it runs at; TestOffPeakCheck checks the rule.
func TestMain(m *testing.M) {
	peakClock = func() time.Time { return time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC) } // a Saturday
	os.Exit(m.Run())
}

func TestOffPeakCheck(t *testing.T) {
	utc := func(day, hour, min int) time.Time { return time.Date(2026, 10, day, hour, min, 0, 0, time.UTC) }
	// 2026-10-07 is a Wednesday; 10-10 a Saturday.
	cases := []struct {
		at   time.Time
		ok   bool
		next string // UTC hh:mm of the next safe start, when refused
	}{
		{utc(7, 8, 9), false, "10:00"},   // inside the morning peak (10:09 CEST)
		{utc(7, 10, 0), true, ""},        // just after it
		{utc(7, 23, 30), false, "04:00"}, // a run would reach 01:00
		{utc(7, 22, 59), true, ""},       // ends 00:59, before 01:00
		{utc(7, 4, 30), false, "10:00"},  // would reach 06:00
		{utc(10, 8, 0), true, ""},        // Saturday: no peak
		{utc(9, 23, 30), true, ""},       // Friday night into Saturday
	}
	for _, c := range cases {
		err := offPeakCheck(c.at)
		if (err == nil) != c.ok {
			t.Errorf("%s: err %v, want ok %v", c.at.Format("Mon 15:04"), err, c.ok)
			continue
		}
		if !c.ok {
			if got := nextOffPeakStart(c.at, runAllowance).UTC().Format("15:04"); got != c.next {
				t.Errorf("%s: next start %s, want %s", c.at.Format("Mon 15:04"), got, c.next)
			}
			if !strings.Contains(err.Error(), "-allow-peak") {
				t.Errorf("the refusal does not say how to override: %v", err)
			}
		}
	}
}

package main

import (
	"context"
	"fmt"
	"io"
	"time"
)

// DeepSeek bills double during its peak hours: 01:00-04:00 and 06:00-10:00
// UTC, Monday to Friday (https://api-docs.deepseek.com/quick_start/pricing,
// read 2026-10-07; Chinese public holidays are off-peak, which this does
// not know, so it errs toward waiting). An automated day spends most of its
// money on DeepSeek (requests, Helix's plans, replays, reflection), so
// `auto` runs only off-peak unless told otherwise.

// peakClock is the clock the off-peak rule reads; tests set it.
var peakClock = time.Now

// peakWindows are the peak hours, in UTC, as [start, end) hours.
var peakWindows = [][2]int{{1, 4}, {6, 10}}

// runAllowance is how long a run is expected to take: it must not reach a
// peak window either.
const runAllowance = 2 * time.Hour

// inPeak reports whether t falls in a peak window.
func inPeak(t time.Time) bool {
	u := t.UTC()
	if u.Weekday() == time.Saturday || u.Weekday() == time.Sunday {
		return false
	}
	for _, w := range peakWindows {
		if u.Hour() >= w[0] && u.Hour() < w[1] {
			return true
		}
	}
	return false
}

// peakWithin reports whether any moment of [t, t+d) is in a peak window,
// checked minute by minute (windows start and end on the hour).
func peakWithin(t time.Time, d time.Duration) bool {
	for m := time.Duration(0); m < d; m += time.Minute {
		if inPeak(t.Add(m)) {
			return true
		}
	}
	return false
}

// nextOffPeakStart is the first minute from t at which a run of d would not
// touch a peak window.
func nextOffPeakStart(t time.Time, d time.Duration) time.Time {
	s := t.Truncate(time.Minute)
	for i := 0; i < 8*24*60; i++ {
		if !peakWithin(s, d) {
			return s
		}
		s = s.Add(time.Minute)
	}
	return s
}

// offPeakCheck refuses to start a run that would pay peak prices.
func offPeakCheck(now time.Time) error {
	if !peakWithin(now, runAllowance) {
		return nil
	}
	next := nextOffPeakStart(now, runAllowance)
	return fmt.Errorf("DeepSeek's peak prices (double) apply now or within the next %s; "+
		"start at %s or later (local time), or pass -allow-peak to pay them",
		runAllowance, next.Local().Format("Mon 15:04"))
}

// cmdOffPeak exits with an error during (or within a run's allowance of)
// DeepSeek's peak hours, for scripts that are about to spend: make collect.
func cmdOffPeak(_ context.Context, _ []string, out io.Writer) error {
	if err := offPeakCheck(peakClock()); err != nil {
		return err
	}
	say(out, "off-peak: DeepSeek is at its lower price for the next %s\n", runAllowance)
	return nil
}

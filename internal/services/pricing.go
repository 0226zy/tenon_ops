package services

import "time"

// peakAt implements the billing calendar: Monday-Friday 09:00-12:00 and
// 14:00-18:00 are peak; all other hours and Saturday/Sunday are off-peak.
func peakAt(t time.Time, loc *time.Location) bool {
	local := t.In(loc)
	if local.Weekday() == time.Saturday || local.Weekday() == time.Sunday {
		return false
	}
	minutes := local.Hour()*60 + local.Minute()
	return (minutes >= 9*60 && minutes < 12*60) || (minutes >= 14*60 && minutes < 18*60)
}

// Package reportclock defines the calendar shared by accounting and reports.
package reportclock

import (
	"time"
	_ "time/tzdata" // Also work on minimal hosts without a zoneinfo installation.
)

const Zone = "Asia/Tehran"

var Iran = func() *time.Location {
	loc, err := time.LoadLocation(Zone)
	if err != nil {
		panic(err)
	}
	return loc
}()

func Date(t time.Time) string { return t.In(Iran).Format("2006-01-02") }
func Midnight(t time.Time) time.Time {
	t = t.In(Iran)
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, Iran)
}

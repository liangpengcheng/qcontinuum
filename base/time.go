package base

import "time"

//UntilTomorrow return second
func UntilTomorrow() int {
	now := time.Now()
	h := 23 - now.Hour()
	m := 59 - now.Minute()
	s := 59 - now.Second()
	return s + m*60 + h*60*60
}

// UntilNextWeek return second
func UntilNextWeek() int {
	now := time.Now()
	w := now.Weekday()
	leftsec := (7-int(w))*24*60*60 - now.Hour()*60*60 - (now.Minute()+5)*60 - now.Second()
	return leftsec
}

// DeltaDays 两个时间相差几天 ，都是unix秒
func DeltaDays(day1Unix, day2Unix int64) int {
	d1 := time.Unix(day1Unix, 0)
	d2 := time.Unix(day2Unix, 0)
	return (d2.Year()-d1.Year())*365 + d2.YearDay() - d1.YearDay()
}

package desk

import (
	"fmt"
	"time"
)

// BeijingLocation 是所有工单时间统一使用的时区（UTC+8）。
var BeijingLocation = time.FixedZone("CST", 8*60*60)

// WorkStartHour / WorkEndHour 定义工作时间 09:00-18:00，
// 每个工作日共有 9*60=540 个可计费分钟。
const (
	WorkStartHour = 9
	WorkEndHour   = 18
	WorkMinutes   = (WorkEndHour - WorkStartHour) * 60
)

// Calendar 保存初始化后不再改变的休息日集合。
// key 使用 "2006-01-02" 格式的北京时间日期。
type Calendar struct {
	Holidays map[string]bool `json:"holidays"`
}

// NewCalendar 创建日历，restDays 为 "YYYY-MM-DD"（北京时间）。
func NewCalendar(restDays []string) (*Calendar, error) {
	c := &Calendar{Holidays: map[string]bool{}}
	for _, d := range restDays {
		day, err := time.ParseInLocation("2006-01-02", d, BeijingLocation)
		if err != nil {
			return nil, fmt.Errorf("invalid rest day %q: %w", d, err)
		}
		if day.Weekday() == time.Saturday || day.Weekday() == time.Sunday {
			return nil, fmt.Errorf("rest day %q is already a weekend", d)
		}
		if c.Holidays[d] {
			return nil, fmt.Errorf("duplicate rest day %q", d)
		}
		c.Holidays[d] = true
	}
	return c, nil
}

// IsWorkingDay 判断某一时刻所在的北京时间日期是否为工作日。
func (c *Calendar) IsWorkingDay(t time.Time) bool {
	d := t.In(BeijingLocation).Format("2006-01-02")
	if c.Holidays[d] {
		return false
	}
	w := t.In(BeijingLocation).Weekday()
	return w >= time.Monday && w <= time.Friday
}

// WorkingMinutes 计算左闭右开区间 [start, end) 内落在工作时间中的分钟数。
// start/end 必须是北京时间下精确到分钟的时刻，且 end 不得早于 start。
func (c *Calendar) WorkingMinutes(start, end time.Time) int {
	if !end.After(start) {
		return 0
	}
	total := 0
	cursor := start
	for cursor.Before(end) {
		loc := cursor.In(BeijingLocation)
		dayStart := time.Date(loc.Year(), loc.Month(), loc.Day(), 0, 0, 0, 0, BeijingLocation)
		ws := dayStart.Add(WorkStartHour * time.Hour)
		we := dayStart.Add(WorkEndHour * time.Hour)
		dayEnd := dayStart.Add(24 * time.Hour)

		if c.IsWorkingDay(cursor) {
			segStart := maxTime(cursor, ws)
			segEnd := minTime(end, minTime(we, dayEnd))
			if segEnd.After(segStart) {
				total += int(segEnd.Sub(segStart) / time.Minute)
			}
		}
		cursor = dayEnd
	}
	return total
}

// ParseMinuteTime 解析 "2006-01-02T15:04" 形式的北京时间，要求精确到分钟。
func ParseMinuteTime(s string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02T15:04", s, BeijingLocation)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid time %q, want YYYY-MM-DDTHH:MM in Beijing time: %w", s, err)
	}
	return t, nil
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func maxTime(a, b time.Time) time.Time {
	if a.After(b) {
		return a
	}
	return b
}

package handlers

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type calendarDay struct {
	Day     int
	Date    string
	InMonth bool
	IsToday bool
	Notes   []calendarNote
}

type calendarNote struct {
	Title string
	URL   string
}

type calendarGridProps struct {
	MonthLabel string
	PrevMonth  string
	NextMonth  string
	ThisMonth  string
	Days       []calendarDay
	DayNames   []string
}

// buildCalendarGrid renders one month of a user's notes as a day grid — the
// profile page's ?view=calendar mode. mParam is the "?m=YYYY-MM" query value
// (empty = current month). includeDrafts is true only for the profile owner.
func (s *Server) buildCalendarGrid(ctx context.Context, userID uuid.UUID, mParam string, includeDrafts bool) (calendarGridProps, error) {
	loc := time.Local
	now := time.Now().In(loc)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	if mParam != "" {
		if t, err := time.ParseInLocation("2006-01", mParam, loc); err == nil {
			month = t
		}
	}
	monthEnd := month.AddDate(0, 1, 0)
	gridStart := month.AddDate(0, 0, -int(month.Weekday()))
	gridEnd := gridStart.AddDate(0, 0, 42)

	byDay, err := s.notesByDay(ctx, userID, gridStart, gridEnd, includeDrafts)
	if err != nil {
		return calendarGridProps{}, err
	}

	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	days := make([]calendarDay, 0, 42)
	for d := gridStart; d.Before(gridEnd); d = d.AddDate(0, 0, 1) {
		key := d.Format("2006-01-02")
		days = append(days, calendarDay{
			Day:     d.Day(),
			Date:    key,
			InMonth: !d.Before(month) && d.Before(monthEnd),
			IsToday: d.Equal(today),
			Notes:   byDay[key],
		})
	}

	return calendarGridProps{
		MonthLabel: month.Format("2006 · 01"),
		PrevMonth:  month.AddDate(0, -1, 0).Format("2006-01"),
		NextMonth:  month.AddDate(0, 1, 0).Format("2006-01"),
		ThisMonth:  time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).Format("2006-01"),
		Days:       days,
		DayNames:   []string{"日", "一", "二", "三", "四", "五", "六"},
	}, nil
}

// calendarCellClass mirrors calendar_page.templ's "calendar-cell{{if not
// .InMonth}} calendar-cell-out{{end}}{{if .IsToday}} calendar-cell-today{{end}}".
func calendarCellClass(d calendarDay) string {
	class := "calendar-cell"
	if !d.InMonth {
		class += " calendar-cell-out"
	}
	if d.IsToday {
		class += " calendar-cell-today"
	}
	return class
}

func (s *Server) notesByDay(ctx context.Context, userID uuid.UUID, start, end time.Time, includeDrafts bool) (map[string][]calendarNote, error) {
	cards, err := s.queryNotesInRange(ctx, userID, start, end, includeDrafts)
	if err != nil {
		return nil, err
	}
	out := map[string][]calendarNote{}
	for _, c := range cards {
		key := time.Unix(c.UpdatedAt, 0).In(time.Local).Format("2006-01-02")
		out[key] = append(out[key], calendarNote{Title: c.Title, URL: c.URL})
	}
	return out, nil
}

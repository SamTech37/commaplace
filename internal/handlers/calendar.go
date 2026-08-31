package handlers

import (
	"context"
	"net/http"
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

// GetCalendar renders the current user's notes laid out on a month grid.
// Defaults to the current month; ?m=YYYY-MM jumps to another month.
func (s *Server) GetCalendar(w http.ResponseWriter, r *http.Request) {
	u := s.requireUser(w, r)
	if u == nil {
		return
	}

	loc := time.Local
	now := time.Now().In(loc)
	month := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc)
	if m := r.URL.Query().Get("m"); m != "" {
		if t, err := time.ParseInLocation("2006-01", m, loc); err == nil {
			month = t
		}
	}
	monthEnd := month.AddDate(0, 1, 0)

	gridStart := month.AddDate(0, 0, -int(month.Weekday()))
	gridEnd := gridStart.AddDate(0, 0, 42)

	byDay, err := s.notesByDay(r.Context(), u.ID, gridStart, gridEnd)
	if err != nil {
		s.renderError(w, r, http.StatusInternalServerError, err.Error())
		return
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

	props := calendarPageProps{
		MonthLabel: month.Format("2006 · 01"),
		PrevMonth:  month.AddDate(0, -1, 0).Format("2006-01"),
		NextMonth:  month.AddDate(0, 1, 0).Format("2006-01"),
		ThisMonth:  time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).Format("2006-01"),
		Days:       days,
		DayNames:   []string{"日", "一", "二", "三", "四", "五", "六"},
	}
	s.renderPage(w, r, pageTitle(navCfg.Calendar), "", nil, calendarPage(props))
}

type calendarPageProps struct {
	MonthLabel string
	PrevMonth  string
	NextMonth  string
	ThisMonth  string
	Days       []calendarDay
	DayNames   []string
}

// calendarCellClass mirrors calendar.html's "calendar-cell{{if not .InMonth}}
// calendar-cell-out{{end}}{{if .IsToday}} calendar-cell-today{{end}}".
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

func (s *Server) notesByDay(ctx context.Context, userID uuid.UUID, start, end time.Time) (map[string][]calendarNote, error) {
	rows, err := s.DB.QueryContext(ctx, `
		SELECT n.slug, n.title, n.updated_at, u.handle
		FROM notes n
		JOIN users u ON u.id = n.author_id
		WHERE n.author_id = $1
		  AND n.deleted_at IS NULL
		  AND n.updated_at >= $2
		  AND n.updated_at < $3
		ORDER BY n.updated_at ASC`, userID, start.Unix(), end.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]calendarNote{}
	for rows.Next() {
		var slug, title, handle string
		var updated int64
		if err := rows.Scan(&slug, &title, &updated, &handle); err != nil {
			return nil, err
		}
		key := time.Unix(updated, 0).In(time.Local).Format("2006-01-02")
		out[key] = append(out[key], calendarNote{
			Title: title,
			URL:   noteURL(handle, slug),
		})
	}
	return out, rows.Err()
}

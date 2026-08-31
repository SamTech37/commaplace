package handlers

import (
	"context"
	"net/http"
	"net/url"
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

// timelineGroup is one date's notes in the timeline view — consecutive
// same-day cards from queryNotesInRange's ascending-time result.
type timelineGroup struct {
	DateLabel string
	Cards     []feedCard
}

// GetCalendar renders the current user's notes for one month, as either a
// month grid or a linear timeline (?view=timeline). Defaults to the current
// month; ?m=YYYY-MM jumps to another month. Both views share the same
// month + time-range query — only the layout differs.
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

	view := r.URL.Query().Get("view")
	if view != "timeline" {
		view = "calendar"
	}

	props := calendarPageProps{
		View:       view,
		MonthLabel: month.Format("2006 · 01"),
		PrevMonth:  month.AddDate(0, -1, 0).Format("2006-01"),
		NextMonth:  month.AddDate(0, 1, 0).Format("2006-01"),
		ThisMonth:  time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, loc).Format("2006-01"),
		CalendarURL: calendarViewURL("calendar", month, r.URL.Query().Get("m")),
		TimelineURL: calendarViewURL("timeline", month, r.URL.Query().Get("m")),
	}

	if view == "timeline" {
		cards, err := s.queryNotesInRange(r.Context(), u.ID, month, monthEnd)
		if err != nil {
			s.renderError(w, r, http.StatusInternalServerError, err.Error())
			return
		}
		for i := range cards {
			// This is always the viewer's own vault; the handle would be
			// redundant on every card (same reasoning as profile.go's
			// loadRecentNotes).
			cards[i].AuthorHandle = ""
			// listCard renders a bulk-delete checkbox when IsDraft is set,
			// meant for profile's drafts tab and its wrapping <form>. There's
			// no such form here, so clear it — an inert checkbox is worse
			// than none.
			cards[i].IsDraft = false
		}
		props.Groups = groupByDay(cards)
	} else {
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
		props.Days = days
		props.DayNames = []string{"日", "一", "二", "三", "四", "五", "六"}
	}

	s.renderPage(w, r, pageTitle(navCfg.Calendar), "", nil, calendarPage(props))
}

// calendarViewURL builds the ?m=...&view=... link for the given view,
// preserving the current month.
func calendarViewURL(view string, month time.Time, mParam string) string {
	v := url.Values{}
	if mParam != "" {
		v.Set("m", mParam)
	} else {
		v.Set("m", month.Format("2006-01"))
	}
	if view != "calendar" {
		v.Set("view", view)
	}
	return "/me/calendar?" + v.Encode()
}

// groupByDay buckets cards (already ordered ascending by UpdatedAt) into
// consecutive same-day groups, newest day first for reading top-to-bottom
// as "most recent first" like every other list surface.
func groupByDay(cards []feedCard) []timelineGroup {
	byDate := map[string][]feedCard{}
	var order []string
	for _, c := range cards {
		key := time.Unix(c.UpdatedAt, 0).In(time.Local).Format("2006-01-02")
		if _, ok := byDate[key]; !ok {
			order = append(order, key)
		}
		byDate[key] = append(byDate[key], c)
	}
	groups := make([]timelineGroup, len(order))
	for i, key := range order {
		t, _ := time.ParseInLocation("2006-01-02", key, time.Local)
		cards := byDate[key]
		for l, r := 0, len(cards)-1; l < r; l, r = l+1, r-1 {
			cards[l], cards[r] = cards[r], cards[l]
		}
		groups[len(order)-1-i] = timelineGroup{
			DateLabel: t.Format("01/02"),
			Cards:     cards,
		}
	}
	return groups
}

type calendarPageProps struct {
	View        string
	MonthLabel  string
	PrevMonth   string
	NextMonth   string
	ThisMonth   string
	CalendarURL string
	TimelineURL string
	Days        []calendarDay
	DayNames    []string
	Groups      []timelineGroup
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
	cards, err := s.queryNotesInRange(ctx, userID, start, end)
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

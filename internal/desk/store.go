package desk

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// stateFile 是数据目录中的持久化文件名。
const stateFile = "supportdesk.json"

// ErrNotInitialized 表示数据目录尚未初始化。
var ErrNotInitialized = errors.New("desk is not initialized; send an init request first")

// ErrAlreadyInitialized 表示日历已经初始化，休息日不可再次指定。
var ErrAlreadyInitialized = errors.New("desk is already initialized; rest days cannot be changed")

// persistedState 是落盘结构。
type persistedState struct {
	Initialized bool               `json:"initialized"`
	Holidays    []string           `json:"holidays,omitempty"`
	Tickets     map[string]*Ticket `json:"tickets"`
}

// Store 管理内存中的工单并负责持久化。
type Store struct {
	dir      string
	cal      *Calendar
	tickets  map[string]*Ticket
	initFlag bool
}

// Load 从数据目录载入状态；目录或文件不存在时返回未初始化的空 Store。
func Load(dir string) (*Store, error) {
	s := &Store{dir: dir, tickets: map[string]*Ticket{}}
	path := filepath.Join(dir, stateFile)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var ps persistedState
	if err := json.Unmarshal(data, &ps); err != nil {
		return nil, fmt.Errorf("corrupt state file: %w", err)
	}
	s.initFlag = ps.Initialized
	if ps.Initialized {
		cal, err := NewCalendar(ps.Holidays)
		if err != nil {
			return nil, err
		}
		s.cal = cal
	}
	if ps.Tickets != nil {
		s.tickets = ps.Tickets
	}
	return s, nil
}

// Initialized 返回是否已完成初始化。
func (s *Store) Initialized() bool { return s.initFlag }

// Calendar 返回日历，未初始化时为 nil。
func (s *Store) Calendar() *Calendar { return s.cal }

// Init 初始化日历（休息日），仅允许在尚无任何工单且未初始化时执行一次。
func (s *Store) Init(restDays []string) (*Calendar, error) {
	if s.initFlag {
		return nil, ErrAlreadyInitialized
	}
	cal, err := NewCalendar(restDays)
	if err != nil {
		return nil, err
	}
	s.cal = cal
	s.initFlag = true
	if err := s.persist(); err != nil {
		return nil, err
	}
	return cal, nil
}

// requireReady 确保已初始化。
func (s *Store) requireReady() error {
	if !s.initFlag {
		return ErrNotInitialized
	}
	return nil
}

// Create 创建工单。任何校验失败都不会修改现有数据。
func (s *Store) Create(id, customer, assignee string, limitMins int, at time.Time) (*Ticket, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	if id == "" {
		return nil, errors.New("missing ticket id")
	}
	if customer == "" {
		return nil, errors.New("missing customer")
	}
	if assignee == "" {
		return nil, errors.New("missing assignee")
	}
	if limitMins <= 0 {
		return nil, errors.New("limit_minutes must be a positive integer")
	}
	if _, exists := s.tickets[id]; exists {
		return nil, fmt.Errorf("duplicate ticket id %q", id)
	}
	t := &Ticket{
		ID:        id,
		Customer:  customer,
		Assignee:  assignee,
		LimitMins: limitMins,
		Status:    StatusProcessing,
		CreatedAt: at,
		History: []HistoryEntry{{
			At:   at,
			From: "",
			To:   StatusProcessing,
		}},
	}
	s.tickets[id] = t
	if err := s.persist(); err != nil {
		delete(s.tickets, id)
		return nil, err
	}
	return t, nil
}

// Transition 变更工单状态。非法转换或时间倒退时返回错误且不修改任何数据。
func (s *Store) Transition(id, to string, at time.Time, reason string) (*Ticket, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	t, ok := s.tickets[id]
	if !ok {
		return nil, fmt.Errorf("ticket %q not found", id)
	}
	if to != StatusProcessing && to != StatusWaiting && to != StatusDone {
		return nil, fmt.Errorf("unknown target status %q", to)
	}
	if !validTransition(t.Status, to) {
		return nil, fmt.Errorf("illegal transition %s -> %s", t.Status, to)
	}
	if at.Before(t.LastOpAt()) {
		return nil, fmt.Errorf("operation time %s goes backwards from %s",
			at.Format("2006-01-02T15:04"), t.LastOpAt().Format("2006-01-02T15:04"))
	}

	snap := t.snapshot()
	prev := t.Status
	if to == StatusDone {
		used, err := t.UsedMinutesAt(at, s.cal)
		if err != nil {
			return nil, err
		}
		t.UsedMins = used
	}
	t.Status = to
	t.History = append(t.History, HistoryEntry{At: at, From: prev, To: to, Reason: reason})
	if err := s.persist(); err != nil {
		*t = *snap
		return nil, err
	}
	return t, nil
}

// View 是查询返回的工单视图。
type View struct {
	ID            string        `json:"id"`
	Customer      string        `json:"customer"`
	Assignee      string        `json:"assignee"`
	Status        string        `json:"status"`
	LimitMinutes  int           `json:"limit_minutes"`
	CreatedAt     string        `json:"created_at"`
	UsedMinutes   int           `json:"used_minutes"`
	RemainingMins int           `json:"remaining_minutes"`
	Overtime      bool          `json:"overtime"`
	History       []HistoryView `json:"history"`
}

// HistoryView 是历史条目的 JSON 形式。
type HistoryView struct {
	At     string `json:"at"`
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}

// Get 查询单个工单，按 at 时刻计算耗时；查询不会写入任何数据。
func (s *Store) Get(id string, at time.Time) (*View, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	t, ok := s.tickets[id]
	if !ok {
		return nil, fmt.Errorf("ticket %q not found", id)
	}
	return s.buildView(t, at)
}

// ListFilter 是列表筛选条件，空值表示不限制。
type ListFilter struct {
	Status   string
	Assignee string
	Overtime *bool // nil 表示不过滤
}

// List 按条件筛选工单，排序：创建时间升序，再按编号升序。
func (s *Store) List(f ListFilter, at time.Time) ([]*View, error) {
	if err := s.requireReady(); err != nil {
		return nil, err
	}
	var latest time.Time
	for _, t := range s.tickets {
		if t.LastOpAt().After(latest) {
			latest = t.LastOpAt()
		}
	}
	if !latest.IsZero() && at.Before(latest) {
		return nil, fmt.Errorf("query time %s is earlier than the latest operation at %s",
			at.Format("2006-01-02T15:04"), latest.Format("2006-01-02T15:04"))
	}

	var picked []*Ticket
	for _, t := range s.tickets {
		if f.Status != "" && t.Status != f.Status {
			continue
		}
		if f.Assignee != "" && t.Assignee != f.Assignee {
			continue
		}
		used, err := t.UsedMinutesAt(at, s.cal)
		if err != nil {
			return nil, fmt.Errorf("ticket %q: %w", t.ID, err)
		}
		if f.Overtime != nil && (used > t.LimitMins) != *f.Overtime {
			continue
		}
		picked = append(picked, t)
	}
	sort.Slice(picked, func(i, j int) bool {
		if !picked[i].CreatedAt.Equal(picked[j].CreatedAt) {
			return picked[i].CreatedAt.Before(picked[j].CreatedAt)
		}
		return picked[i].ID < picked[j].ID
	})
	views := make([]*View, 0, len(picked))
	for _, t := range picked {
		v, err := s.buildView(t, at)
		if err != nil {
			return nil, err
		}
		views = append(views, v)
	}
	return views, nil
}

func (s *Store) buildView(t *Ticket, at time.Time) (*View, error) {
	used, err := t.UsedMinutesAt(at, s.cal)
	if err != nil {
		return nil, err
	}
	remaining := t.LimitMins - used
	if remaining < 0 {
		remaining = 0
	}
	hv := make([]HistoryView, 0, len(t.History))
	for _, h := range t.History {
		hv = append(hv, HistoryView{
			At:     h.At.Format("2006-01-02T15:04"),
			From:   h.From,
			To:     h.To,
			Reason: h.Reason,
		})
	}
	return &View{
		ID:            t.ID,
		Customer:      t.Customer,
		Assignee:      t.Assignee,
		Status:        t.Status,
		LimitMinutes:  t.LimitMins,
		CreatedAt:     t.CreatedAt.Format("2006-01-02T15:04"),
		UsedMinutes:   used,
		RemainingMins: remaining,
		Overtime:      used > t.LimitMins,
		History:       hv,
	}, nil
}

// persist 将状态原子写入磁盘：先写临时文件再重命名。
func (s *Store) persist() error {
	if err := os.MkdirAll(s.dir, 0o755); err != nil {
		return err
	}
	holidays := make([]string, 0, len(s.cal.Holidays))
	for d := range s.cal.Holidays {
		holidays = append(holidays, d)
	}
	sort.Strings(holidays)
	ps := persistedState{
		Initialized: s.initFlag,
		Holidays:    holidays,
		Tickets:     s.tickets,
	}
	data, err := json.MarshalIndent(&ps, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(s.dir, stateFile+".tmp")
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(s.dir, stateFile))
}

package desk

import (
	"testing"
	"time"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	v, err := ParseMinuteTime(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return v
}

func newTestStore(t *testing.T, restDays ...string) *Store {
	t.Helper()
	s, err := Load(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Init(restDays); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestWorkingMinutesWithinDay(t *testing.T) {
	cal, _ := NewCalendar(nil)
	got := cal.WorkingMinutes(mustTime(t, "2026-09-28T09:00"), mustTime(t, "2026-09-28T18:00"))
	if got != 540 {
		t.Fatalf("full workday = %d, want 540", got)
	}
	got = cal.WorkingMinutes(mustTime(t, "2026-09-28T08:30"), mustTime(t, "2026-09-28T19:30"))
	if got != 540 {
		t.Fatalf("clamped workday = %d, want 540", got)
	}
}

func TestWorkingMinutesWeekendAndHoliday(t *testing.T) {
	cal, _ := NewCalendar([]string{"2026-09-30"}) // 周三休息
	sat := mustTime(t, "2026-09-26T09:00")
	sun := mustTime(t, "2026-09-27T18:00")
	if got := cal.WorkingMinutes(sat, sun); got != 0 {
		t.Fatalf("weekend = %d, want 0", got)
	}
	holStart := mustTime(t, "2026-09-30T00:00")
	holEnd := mustTime(t, "2026-10-01T00:00")
	if got := cal.WorkingMinutes(holStart, holEnd); got != 0 {
		t.Fatalf("holiday = %d, want 0", got)
	}
	// 跨夜：周一 17:00 到周二 10:00 = 60 + 60 = 120
	got := cal.WorkingMinutes(mustTime(t, "2026-09-28T17:00"), mustTime(t, "2026-09-29T10:00"))
	if got != 120 {
		t.Fatalf("overnight = %d, want 120", got)
	}
}

func TestFullLifecycle(t *testing.T) {
	s := newTestStore(t)
	create := mustTime(t, "2026-09-28T09:00")
	tk, err := s.Create("T-1", "ACME", "alice", 120, create)
	if err != nil {
		t.Fatal(err)
	}
	if tk.Status != StatusProcessing {
		t.Fatalf("initial status = %q", tk.Status)
	}

	// 120 分钟恰好耗尽：不算超时，剩余 0。
	v, err := s.Get("T-1", mustTime(t, "2026-09-28T11:00"))
	if err != nil {
		t.Fatal(err)
	}
	if v.UsedMinutes != 120 || v.RemainingMins != 0 || v.Overtime {
		t.Fatalf("at limit: used=%d remain=%d overtime=%v", v.UsedMinutes, v.RemainingMins, v.Overtime)
	}

	// 再一分钟：超时。
	v, _ = s.Get("T-1", mustTime(t, "2026-09-28T11:01"))
	if !v.Overtime || v.UsedMinutes != 121 || v.RemainingMins != 0 {
		t.Fatalf("overtime: used=%d remain=%d overtime=%v", v.UsedMinutes, v.RemainingMins, v.Overtime)
	}

	// 等待客户：夜间不计时。
	if _, err := s.Transition("T-1", StatusWaiting, mustTime(t, "2026-09-28T18:00"), "need logs"); err != nil {
		t.Fatal(err)
	}
	v, _ = s.Get("T-1", mustTime(t, "2026-09-29T17:00"))
	if v.UsedMinutes != 540 { // 09:00-18:00 一整天
		t.Fatalf("waiting should pause: used=%d want 540", v.UsedMinutes)
	}

	// 等待只能恢复处理。
	if _, err := s.Transition("T-1", StatusDone, mustTime(t, "2026-09-29T17:00"), ""); err == nil {
		t.Fatal("waiting -> done should be rejected")
	}
	if _, err := s.Transition("T-1", StatusWaiting, mustTime(t, "2026-09-29T17:01"), ""); err == nil {
		t.Fatal("waiting -> waiting should be rejected")
	}

	if _, err := s.Transition("T-1", StatusProcessing, mustTime(t, "2026-09-29T17:00"), ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Transition("T-1", StatusDone, mustTime(t, "2026-09-29T18:00"), ""); err != nil {
		t.Fatal(err)
	}

	// 完成后冻结，且不得再次变更。
	v, _ = s.Get("T-1", mustTime(t, "2026-12-31T18:00"))
	if v.UsedMinutes != 600 || !v.Overtime { // 540 + 60，超出 120 限额
		t.Fatalf("frozen: used=%d overtime=%v, want 600/true", v.UsedMinutes, v.Overtime)
	}
	if _, err := s.Transition("T-1", StatusProcessing, mustTime(t, "2026-12-31T18:01"), ""); err == nil {
		t.Fatal("transition after done should be rejected")
	}
}

func TestWaitingPausesAcrossHoliday(t *testing.T) {
	s := newTestStore(t, "2026-09-30")
	s.Create("H1", "cust", "bob", 10000, mustTime(t, "2026-09-28T09:00"))
	// 周一 12:00 进入等待（已用 180），周五 10-02 的 12:00 恢复（仍为 180）。
	s.Transition("H1", StatusWaiting, mustTime(t, "2026-09-28T12:00"), "")
	s.Transition("H1", StatusProcessing, mustTime(t, "2026-10-02T12:00"), "")
	v, err := s.Get("H1", mustTime(t, "2026-10-02T18:00"))
	if err != nil {
		t.Fatal(err)
	}
	// 180（周一上午）+ 360（周五下午）= 540；休息日 09-30、周末、周二至周四均在等待中。
	if v.UsedMinutes != 540 {
		t.Fatalf("used=%d, want 540", v.UsedMinutes)
	}
}

func TestRejections(t *testing.T) {
	s := newTestStore(t)
	at := mustTime(t, "2026-09-28T09:00")

	if _, err := s.Create("", "c", "a", 60, at); err == nil {
		t.Error("empty id should fail")
	}
	if _, err := s.Create("X", "", "a", 60, at); err == nil {
		t.Error("empty customer should fail")
	}
	if _, err := s.Create("X", "c", "", 60, at); err == nil {
		t.Error("empty assignee should fail")
	}
	if _, err := s.Create("X", "c", "a", 0, at); err == nil {
		t.Error("zero limit should fail")
	}
	if _, err := s.Create("X", "c", "a", -5, at); err == nil {
		t.Error("negative limit should fail")
	}
	if _, err := s.Create("X", "c", "a", 60, at); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("X", "c2", "a2", 60, at); err == nil {
		t.Error("duplicate id should fail")
	}

	// 时间倒退。
	if _, err := s.Transition("X", StatusWaiting, mustTime(t, "2026-09-28T08:59"), ""); err == nil {
		t.Error("backwards transition should fail")
	}
	// 查询早于最近操作。
	if _, err := s.Get("X", mustTime(t, "2026-09-28T08:00")); err == nil {
		t.Error("query before last op should fail")
	}
	// 非法转换：processing -> processing。
	if _, err := s.Transition("X", StatusProcessing, at, ""); err == nil {
		t.Error("processing -> processing should fail")
	}
	// 不存在的工单。
	if _, err := s.Get("nope", at); err == nil {
		t.Error("missing ticket should fail")
	}
	// 失败后状态不变。
	v, _ := s.Get("X", mustTime(t, "2026-09-28T10:00"))
	if v.Status != StatusProcessing || len(v.History) != 1 {
		t.Fatalf("failed ops must not mutate: status=%s history=%d", v.Status, len(v.History))
	}
}

func TestListFiltersAndSorting(t *testing.T) {
	s := newTestStore(t)
	t0 := mustTime(t, "2026-09-28T09:00")
	s.Create("B", "c", "alice", 30, t0)
	s.Create("A", "c", "alice", 30, t0)
	s.Create("C", "c", "bob", 1000, t0.Add(time.Minute))

	// 09:00 创建的 A、B 到 10:00 已用 60 > 30，超时；C 未超时。
	at := mustTime(t, "2026-09-28T10:00")
	over := true
	views, err := s.List(ListFilter{Overtime: &over}, at)
	if err != nil {
		t.Fatal(err)
	}
	if len(views) != 2 || views[0].ID != "A" || views[1].ID != "B" {
		ids := []string{}
		for _, v := range views {
			ids = append(ids, v.ID)
		}
		t.Fatalf("overtime filter/order = %v, want [A B]", ids)
	}

	views, _ = s.List(ListFilter{Assignee: "bob"}, at)
	if len(views) != 1 || views[0].ID != "C" {
		t.Fatalf("assignee filter failed: %+v", views)
	}

	// 查询时间早于全局最近操作应报错。
	if _, err := s.List(ListFilter{}, mustTime(t, "2026-09-28T09:00")); err == nil {
		t.Error("list before latest op should fail")
	}
}

func TestPersistenceAndRestart(t *testing.T) {
	dir := t.TempDir()
	s, _ := Load(dir)
	s.Init([]string{"2026-10-01"})
	s.Create("R1", "cust", "alice", 1000, mustTime(t, "2026-09-28T09:00"))
	s.Transition("R1", StatusWaiting, mustTime(t, "2026-09-28T12:00"), "pending customer")

	// 重新打开：日历、工单与历史均保留。
	s2, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !s2.Initialized() || !s2.Calendar().Holidays["2026-10-01"] {
		t.Fatal("calendar not persisted")
	}
	if _, err := s2.Init(nil); err != ErrAlreadyInitialized {
		t.Fatalf("re-init err = %v, want ErrAlreadyInitialized", err)
	}
	v, err := s2.Get("R1", mustTime(t, "2026-10-02T18:00"))
	if err != nil {
		t.Fatal(err)
	}
	if v.Status != StatusWaiting || v.UsedMinutes != 180 || len(v.History) != 2 {
		t.Fatalf("restart state wrong: %+v", v)
	}

	// 重开后可继续处理；休息日 10-01 不计。
	s2.Transition("R1", StatusProcessing, mustTime(t, "2026-10-02T09:00"), "")
	v, _ = s2.Get("R1", mustTime(t, "2026-10-02T10:00"))
	if v.UsedMinutes != 240 {
		t.Fatalf("after resume used=%d, want 240", v.UsedMinutes)
	}
}

func TestUninitializedRejectsOps(t *testing.T) {
	s, _ := Load(t.TempDir())
	at := mustTime(t, "2026-09-28T09:00")
	if _, err := s.Create("U1", "c", "a", 60, at); err != ErrNotInitialized {
		t.Fatalf("create before init err = %v", err)
	}
}

func TestInvalidRestDays(t *testing.T) {
	if _, err := NewCalendar([]string{"2026-09-26"}); err == nil {
		t.Error("weekend rest day should fail")
	}
	if _, err := NewCalendar([]string{"not-a-date"}); err == nil {
		t.Error("bad date should fail")
	}
	if _, err := NewCalendar([]string{"2026-09-29", "2026-09-29"}); err == nil {
		t.Error("duplicate rest day should fail")
	}
}

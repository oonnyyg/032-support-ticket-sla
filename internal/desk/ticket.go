package desk

import (
	"fmt"
	"time"
)

// 工单状态常量。
const (
	StatusProcessing = "processing"       // 处理中：累计服务耗时
	StatusWaiting    = "waiting_customer" // 等待客户：暂停计时
	StatusDone       = "done"             // 已完成：耗时冻结
)

// HistoryEntry 记录一次状态变更（创建工单视为第一次变更）。
type HistoryEntry struct {
	At     time.Time `json:"at"`
	From   string    `json:"from"` // 创建时为空字符串
	To     string    `json:"to"`
	Reason string    `json:"reason,omitempty"`
}

// Ticket 是工单的持久化结构。
type Ticket struct {
	ID        string         `json:"id"`
	Customer  string         `json:"customer"`
	Assignee  string         `json:"assignee"`
	LimitMins int            `json:"limit_minutes"`
	Status    string         `json:"status"`
	CreatedAt time.Time      `json:"created_at"`
	History   []HistoryEntry `json:"history"`
	UsedMins  int            `json:"used_minutes"` // 完成时冻结；未完成时为 0，由历史回放计算
}

// LastOpAt 返回最近一次操作时间。
func (t *Ticket) LastOpAt() time.Time {
	return t.History[len(t.History)-1].At
}

// validTransition 校验状态转换是否合法。
func validTransition(from, to string) bool {
	switch from {
	case StatusProcessing:
		return to == StatusWaiting || to == StatusDone
	case StatusWaiting:
		return to == StatusProcessing
	case StatusDone:
		return false
	}
	return false
}

// UsedMinutesAt 按历史回放计算截至 at 时刻工单在处理中状态下
// 累计的工作时间分钟数；已完成工单直接返回冻结值。
func (t *Ticket) UsedMinutesAt(at time.Time, cal *Calendar) (int, error) {
	if t.Status == StatusDone {
		return t.UsedMins, nil
	}
	if at.Before(t.LastOpAt()) {
		return 0, fmt.Errorf("query time %s is earlier than last operation at %s",
			at.Format("2006-01-02T15:04"), t.LastOpAt().Format("2006-01-02T15:04"))
	}
	used := 0
	for i := 0; i < len(t.History); i++ {
		if t.History[i].To != StatusProcessing {
			continue
		}
		segStart := t.History[i].At
		var segEnd time.Time
		if i+1 < len(t.History) {
			segEnd = t.History[i+1].At
		} else {
			segEnd = at
		}
		if segEnd.After(segStart) {
			used += cal.WorkingMinutes(segStart, segEnd)
		}
	}
	return used, nil
}

// snapshot 用于失败不修改：在变更前深拷贝工单。
func (t *Ticket) snapshot() *Ticket {
	cp := *t
	cp.History = append([]HistoryEntry(nil), t.History...)
	return &cp
}

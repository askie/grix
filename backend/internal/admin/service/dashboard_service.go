package service

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/askie/grix/backend/internal/model"
	"github.com/askie/grix/backend/internal/store"
)

const dashboardRegistrationDays = 14
const dashboardTimeZone = "Asia/Shanghai"

type DashboardStats struct {
	TotalUsers       int64                   `json:"total_users"`
	OnlineUsers      int64                   `json:"online_users"`
	OnlineAgents     int64                   `json:"online_agents"`
	DailyRegistrants []DailyRegistrationStat `json:"daily_registrants"`
}

type DailyRegistrationStat struct {
	Date  string `json:"date"`
	Count int64  `json:"count"`
}

type dailyRegistrationRow struct {
	Date  string
	Count int64
}

func DashboardOverview() (DashboardStats, error) {
	var totalUsers int64
	if err := store.Read().
		Model(&model.User{}).
		Where("status <> ?", model.UserStatusDeleted).
		Count(&totalUsers).Error; err != nil {
		return DashboardStats{}, err
	}

	daily, err := loadDailyRegistrants(dashboardRegistrationDays, time.Now())
	if err != nil {
		return DashboardStats{}, err
	}
	onlineUsers, err := countOnlineUsersFromRedis(context.Background())
	if err != nil {
		return DashboardStats{}, err
	}
	onlineAgents, err := countOnlineAgentsFromRedis(context.Background())
	if err != nil {
		return DashboardStats{}, err
	}

	return DashboardStats{
		TotalUsers:       totalUsers,
		OnlineUsers:      onlineUsers,
		OnlineAgents:     onlineAgents,
		DailyRegistrants: daily,
	}, nil
}

func loadDailyRegistrants(days int, now time.Time) ([]DailyRegistrationStat, error) {
	if days <= 0 {
		days = dashboardRegistrationDays
	}
	loc, err := time.LoadLocation(dashboardTimeZone)
	if err != nil {
		loc = time.FixedZone(dashboardTimeZone, 8*60*60)
	}
	localNow := now.In(loc)
	startDay := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc).AddDate(0, 0, -(days - 1))
	startUTC := startDay.UTC()

	db := store.Read()
	dateExpr := "date(created_at)"
	switch db.Dialector.Name() {
	case "postgres":
		dateExpr = "date((created_at AT TIME ZONE 'UTC') AT TIME ZONE 'Asia/Shanghai')"
	case "sqlite":
		dateExpr = "date(created_at, '+8 hours')"
	}
	rows := make([]dailyRegistrationRow, 0, days)
	err = db.
		Model(&model.User{}).
		Select(dateExpr+" AS date, count(*) AS count").
		Where("status <> ? AND created_at >= ?", model.UserStatusDeleted, startUTC).
		Group(dateExpr).
		Order("date ASC").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}

	byDate := make(map[string]int64, len(rows))
	for _, row := range rows {
		byDate[normalizeDateString(row.Date)] = row.Count
	}

	out := make([]DailyRegistrationStat, 0, days)
	for i := 0; i < days; i++ {
		day := startDay.AddDate(0, 0, i).Format("2006-01-02")
		out = append(out, DailyRegistrationStat{
			Date:  day,
			Count: byDate[day],
		})
	}
	return out, nil
}

func normalizeDateString(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= len("2006-01-02") {
		return value[:len("2006-01-02")]
	}
	return value
}

func countOnlineUsersFromRedis(ctx context.Context) (int64, error) {
	ids, err := onlineUserIDsFromRedis(ctx)
	if err != nil {
		return 0, err
	}
	return int64(len(ids)), nil
}

// onlineUserIDsFromRedis returns the distinct users with a live WebSocket
// presence record. A user may have multiple devices connected at once.
func onlineUserIDsFromRedis(ctx context.Context) ([]int64, error) {
	if store.RDB == nil {
		return nil, nil
	}
	seen := map[int64]struct{}{}
	iter := store.RDB.Scan(ctx, 0, "im:ws:alive:*", 200).Iterator()
	for iter.Next(ctx) {
		parts := strings.Split(iter.Val(), ":")
		if len(parts) < 4 {
			continue
		}
		userID, err := strconv.ParseInt(parts[3], 10, 64)
		if err != nil || userID <= 0 {
			continue
		}
		seen[userID] = struct{}{}
	}
	if err := iter.Err(); err != nil {
		return nil, err
	}
	ids := make([]int64, 0, len(seen))
	for userID := range seen {
		ids = append(ids, userID)
	}
	return ids, nil
}

func countOnlineAgentsFromRedis(ctx context.Context) (int64, error) {
	if store.RDB == nil {
		return 0, nil
	}
	seen := map[int64]struct{}{}
	iter := store.RDB.Scan(ctx, 0, "im:agent_api:route:*", 200).Iterator()
	for iter.Next(ctx) {
		suffix := strings.TrimPrefix(iter.Val(), "im:agent_api:route:")
		if suffix == "" {
			continue
		}
		agentIDText := strings.SplitN(suffix, ":", 2)[0]
		agentID, err := strconv.ParseInt(agentIDText, 10, 64)
		if err != nil || agentID <= 0 {
			continue
		}
		seen[agentID] = struct{}{}
	}
	if err := iter.Err(); err != nil {
		return 0, err
	}
	return int64(len(seen)), nil
}

// Package trafficreport implements scheduled reports without executing plugin code.
package trafficreport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/komari-monitor/komari/database/auditlog"
	"github.com/komari-monitor/komari/database/clients"
	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/internal/config"
	"github.com/komari-monitor/komari/internal/metricstore"
	"github.com/komari-monitor/komari/internal/scheduler"
	"github.com/komari-monitor/komari/pkg/metric"
	logger "github.com/komari-monitor/komari/utils/log"
	"github.com/komari-monitor/komari/utils/messageSender"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const ConfigKey = "traffic_report"
const LogType = "traffic-report"
const jobPrefix = "builtin:traffic-report:"

type Configuration struct {
	Enabled        bool     `json:"enabled"`
	AllNodes       bool     `json:"all_nodes"`
	Nodes          []string `json:"nodes"`
	Template       string   `json:"template"`
	DailyEnabled   bool     `json:"daily_enabled"`
	DailyCron      string   `json:"daily_cron"`
	WeeklyEnabled  bool     `json:"weekly_enabled"`
	WeeklyCron     string   `json:"weekly_cron"`
	MonthlyEnabled bool     `json:"monthly_enabled"`
	MonthlyCron    string   `json:"monthly_cron"`
}

func DefaultConfiguration() Configuration {
	return Configuration{Nodes: []string{}, DailyEnabled: true, DailyCron: "0 9 * * *", WeeklyCron: "0 9 * * 1", MonthlyCron: "0 9 1 * *"}
}

var settingsMu sync.Mutex
var sendMu sync.Mutex

func GetConfiguration() (Configuration, error) {
	cfg := DefaultConfiguration()
	saved, err := config.GetAs[json.RawMessage](ConfigKey)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return cfg, nil
	}
	if err != nil {
		return cfg, err
	}
	if err := json.Unmarshal(saved, &cfg); err != nil {
		return cfg, err
	}
	if cfg.Nodes == nil {
		cfg.Nodes = []string{}
	}
	return cfg, nil
}

func Validate(cfg *Configuration) error {
	if len(cfg.Nodes) > 10000 || len(cfg.Template) > 65536 {
		return fmt.Errorf("too many nodes or notification template too long")
	}
	for _, expression := range []*string{&cfg.DailyCron, &cfg.WeeklyCron, &cfg.MonthlyCron} {
		*expression = strings.TrimSpace(*expression)
		if strings.HasPrefix(strings.ToLower(*expression), "@every") {
			*expression = "@every " + strings.TrimSpace((*expression)[6:])
		}
		if _, err := scheduler.Parse(*expression); err != nil {
			return fmt.Errorf("invalid report schedule %q: %w", *expression, err)
		}
	}
	seen := map[string]bool{}
	nodes := make([]string, 0, len(cfg.Nodes))
	for _, id := range cfg.Nodes {
		id = strings.TrimSpace(id)
		if id != "" && !seen[id] {
			seen[id] = true
			nodes = append(nodes, id)
		}
	}
	cfg.Nodes = nodes
	return nil
}

// Migrate copies the old configuration once. Original rows/files remain intact
// for rollback, and an existing built-in configuration always wins.
func Migrate() error {
	db := dbcore.GetDBInstance()
	var existing config.ConfigItem
	err := db.First(&existing, "key = ?", ConfigKey).Error
	if err == nil {
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if !db.Migrator().HasTable(&models.PluginConfiguration{}) {
		return nil
	}
	var legacy models.PluginConfiguration
	err = db.First(&legacy, "short = ?", "traffic-report").Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	cfg := DefaultConfiguration()
	if err := json.Unmarshal([]byte(legacy.Data), &cfg); err != nil {
		return fmt.Errorf("read legacy traffic report configuration: %w", err)
	}
	state, err := os.ReadFile("./data/plugin/state.json")
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err == nil {
		var status struct {
			Plugins map[string]struct {
				Enabled bool `json:"enabled"`
			} `json:"plugins"`
		}
		if err := json.Unmarshal(state, &status); err != nil {
			return fmt.Errorf("read legacy traffic report enabled state: %w", err)
		}
		cfg.Enabled = status.Plugins["traffic-report"].Enabled
	}
	if err := Validate(&cfg); err != nil {
		return err
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	result := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&config.ConfigItem{Key: ConfigKey, Value: string(data)})
	if result.Error == nil && result.RowsAffected > 0 {
		auditlog.EventLog(LogType, "已将原流量报告插件配置迁移为内置配置；原始数据已保留。")
	}
	return result.Error
}

func SaveConfiguration(cfg Configuration) error {
	if err := Validate(&cfg); err != nil {
		return err
	}
	settingsMu.Lock()
	defer settingsMu.Unlock()
	if err := config.Set(ConfigKey, cfg); err != nil {
		return err
	}
	if err := schedule(cfg); err != nil {
		return err
	}
	auditlog.EventLog(LogType, "流量报告配置已保存，发送计划已更新。")
	return nil
}

func Start() error {
	if err := Migrate(); err != nil {
		return err
	}
	cfg, err := GetConfiguration()
	if err != nil {
		return err
	}
	if err := Validate(&cfg); err != nil {
		return err
	}
	return schedule(cfg)
}

func schedule(cfg Configuration) error {
	scheduler.RemovePrefix(jobPrefix)
	if !cfg.Enabled {
		return nil
	}
	jobs := []struct {
		kind, expression string
		enabled          bool
	}{
		{"daily", cfg.DailyCron, cfg.DailyEnabled},
		{"weekly", cfg.WeeklyCron, cfg.WeeklyEnabled},
		{"monthly", cfg.MonthlyCron, cfg.MonthlyEnabled},
	}
	for _, job := range jobs {
		if !job.enabled {
			continue
		}
		kind := job.kind
		if err := scheduler.AddContextFunc(jobPrefix+kind, job.expression, false, func(ctx context.Context) {
			sendMu.Lock()
			defer sendMu.Unlock()
			ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
			defer cancel()
			if ctx.Err() != nil {
				return
			}
			if err := sendReport(ctx, kind); err != nil {
				logger.Error("traffic-report", "report failed", "period", kind, "error", err)
				auditlog.EventLog(LogType, reportLabel(kind)+"发送失败："+err.Error())
			}
		}); err != nil {
			return err
		}
	}
	return nil
}

func periodRange(kind string, now time.Time) (time.Time, time.Time) {
	now = now.In(time.Local)
	end := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	switch kind {
	case "daily":
		return end.AddDate(0, 0, -1), end
	case "weekly":
		end = end.AddDate(0, 0, -(int(now.Weekday())+6)%7)
		return end.AddDate(0, 0, -7), end
	default:
		end = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		return end.AddDate(0, -1, 0), end
	}
}

func reportLabel(kind string) string {
	switch kind {
	case "daily":
		return "日报"
	case "weekly":
		return "周报"
	default:
		return "月报"
	}
}

func formatBytes(value float64) string {
	units := []string{"B", "KB", "MB", "GB", "TB", "PB"}
	i := 0
	for value >= 1024 && i < len(units)-1 {
		value /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f B", value)
	}
	return fmt.Sprintf("%.2f %s", value, units[i])
}

func formatTraffic(up, down float64) string {
	return fmt.Sprintf("↑ %s + ↓ %s = %s", formatBytes(up), formatBytes(down), formatBytes(up+down))
}

var templatePlaceholder = regexp.MustCompile(`\{\{([a-zA-Z0-9_]+)\}\}`)

func renderTemplate(template string, values map[string]string) string {
	return templatePlaceholder.ReplaceAllStringFunc(template, func(placeholder string) string {
		if value, ok := values[placeholder[2:len(placeholder)-2]]; ok {
			return value
		}
		return placeholder
	})
}

func sendReport(ctx context.Context, kind string) error {
	cfg, err := GetConfiguration()
	if err != nil {
		return err
	}
	periodEnabled := map[string]bool{"daily": cfg.DailyEnabled, "weekly": cfg.WeeklyEnabled, "monthly": cfg.MonthlyEnabled}[kind]
	if !cfg.Enabled || !periodEnabled {
		return nil
	}
	notifications, err := config.GetAs[bool](config.NotificationEnabledKey)
	if err != nil {
		return err
	}
	if !notifications {
		auditlog.EventLog(LogType, reportLabel(kind)+"已跳过：通知总开关已关闭。")
		return nil
	}
	all, err := clients.GetAllClientBasicInfo()
	if err != nil {
		return err
	}
	selected := make(map[string]bool, len(cfg.Nodes))
	for _, id := range cfg.Nodes {
		selected[id] = true
	}
	sort.SliceStable(all, func(i, j int) bool {
		if all[i].Weight == all[j].Weight {
			return all[i].UUID < all[j].UUID
		}
		return all[i].Weight < all[j].Weight
	})
	ids := []string{}
	names := map[string]string{}
	for _, node := range all {
		if !cfg.AllNodes && !selected[node.UUID] {
			continue
		}
		ids = append(ids, node.UUID)
		name := node.Name
		if name == "" {
			name = node.UUID
		}
		names[node.UUID] = name
	}
	if len(ids) == 0 {
		auditlog.EventLog(LogType, reportLabel(kind)+"已跳过：未选择有效节点。")
		return nil
	}
	start, end := periodRange(kind, time.Now())
	store := metricstore.GetStore()
	if store == nil {
		return fmt.Errorf("metric store is not initialized")
	}
	interval := store.CompatibleSeriesInterval(start, time.Now(), time.Hour)
	result, err := store.SeriesBatch(ctx, metric.BatchSeriesQuery{
		Specs: []metric.BatchSeriesSpec{
			{MetricName: "traffic.up", Aggregations: []metric.Aggregation{metric.AggSum}, Interval: interval, PreserveSeries: true},
			{MetricName: "traffic.down", Aggregations: []metric.Aggregation{metric.AggSum}, Interval: interval, PreserveSeries: true},
		}, EntityIDs: ids, Start: start, End: end.Add(-time.Nanosecond), Order: metric.OrderAsc,
	}, time.Now())
	if err != nil {
		return err
	}
	type usage struct {
		up, down float64
		hasData  bool
	}
	traffic := map[string]usage{}
	for _, key := range []string{"traffic.up", "traffic.down"} {
		for _, point := range result.Values[key][metric.AggSum] {
			if point.Count == 0 || math.IsNaN(point.Value) || math.IsInf(point.Value, 0) {
				continue
			}
			value := traffic[point.EntityID]
			value.hasData = true
			if key == "traffic.up" {
				value.up += point.Value
			} else {
				value.down += point.Value
			}
			traffic[point.EntityID] = value
		}
	}
	lines := []string{}
	var totalUp, totalDown float64
	missing := 0
	for _, id := range ids {
		value := traffic[id]
		if !value.hasData {
			lines = append(lines, "• "+names[id]+"：无数据")
			missing++
			continue
		}
		totalUp += value.up
		totalDown += value.down
		lines = append(lines, "• "+names[id]+"："+formatTraffic(value.up, value.down))
	}
	startText, endText := start.Format("2006-01-02 15:04:05"), end.Format("2006-01-02 15:04:05")
	message := strings.Join([]string{reportLabel(kind), startText + " ~ " + endText, "", strings.Join(lines, "\n"), "", "总计：" + formatTraffic(totalUp, totalDown)}, "\n")
	event := map[string]string{"daily": "DReport", "weekly": "WReport", "monthly": "MReport"}[kind]
	emoji := map[string]string{"daily": "📊", "weekly": "📈", "monthly": "📅"}[kind]
	now := time.Now().UTC()
	if strings.TrimSpace(cfg.Template) != "" {
		message = renderTemplate(strings.TrimSpace(cfg.Template), map[string]string{
			"period": reportLabel(kind), "start": startText, "end": endText, "message": message,
			"nodes": strings.Join(lines, "\n"), "event": event, "emoji": emoji, "time": now.Format(time.RFC3339Nano),
		})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := messageSender.SendNotification(models.EventMessage{Event: event, Time: now, Emoji: emoji, Message: message}); err != nil {
		return err
	}
	auditlog.EventLog(LogType, fmt.Sprintf("%s发送成功：%s ~ %s，%d 个节点（%d 个无数据）。", reportLabel(kind), startText, endText, len(ids), missing))
	return nil
}

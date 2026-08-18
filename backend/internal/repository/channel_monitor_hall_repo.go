package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"

	"github.com/lib/pq"
)

// 供应商大厅的只读查询。
//
// 单独成文件：这是本站为大厅页加的扩展，跟上游 rebase 时冲突面最小。

// channelMonitorHallQuery 取「已启用且勾选了在大厅展示」的监控项。
//
// LEFT JOIN groups 而不是 INNER JOIN：group_id 刻意没建外键，
// 分组被删除后会留下悬空 ID，此时仍要把监控项列出来，
// 由展示层回退到 group_name 字符串。
const channelMonitorHallQuery = `
	SELECT m.id,
	       m.name,
	       m.provider,
	       m.api_mode,
	       m.primary_model,
	       m.extra_models,
	       m.group_name,
	       m.group_id,
	       m.public_note,
	       m.report_url,
	       m.expected_input_tokens,
	       m.last_checked_at,
	       m.interval_seconds,
	       g.platform,
	       g.name
	FROM channel_monitors m
	LEFT JOIN groups g ON g.id = m.group_id
	WHERE m.enabled = TRUE AND m.public_visible = TRUE
	ORDER BY m.id
`

// ListProviderHall 见 service.ChannelMonitorRepository 接口说明。
func (r *channelMonitorRepository) ListProviderHall(ctx context.Context) ([]*service.ChannelMonitorHallRow, error) {
	rows, err := r.db.QueryContext(ctx, channelMonitorHallQuery)
	if err != nil {
		return nil, fmt.Errorf("query provider hall monitors: %w", err)
	}
	defer func() { _ = rows.Close() }()

	out := make([]*service.ChannelMonitorHallRow, 0)
	for rows.Next() {
		item, err := scanChannelMonitorHallRow(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func scanChannelMonitorHallRow(rows *sql.Rows) (*service.ChannelMonitorHallRow, error) {
	var (
		row           service.ChannelMonitorHallRow
		extraModels   []byte
		groupID       sql.NullInt64
		publicNote    sql.NullString
		reportURL     sql.NullString
		expectedInput sql.NullInt64
		lastCheckedAt sql.NullTime
		groupPlatform sql.NullString
		groupRealName sql.NullString
	)
	if err := rows.Scan(
		&row.ID,
		&row.Name,
		&row.Provider,
		&row.APIMode,
		&row.PrimaryModel,
		&extraModels,
		&row.GroupName,
		&groupID,
		&publicNote,
		&reportURL,
		&expectedInput,
		&lastCheckedAt,
		&row.IntervalSeconds,
		&groupPlatform,
		&groupRealName,
	); err != nil {
		return nil, fmt.Errorf("scan provider hall row: %w", err)
	}

	models, err := decodeChannelMonitorExtraModels(extraModels)
	if err != nil {
		return nil, err
	}
	row.ExtraModels = models

	if groupID.Valid {
		id := groupID.Int64
		row.GroupID = &id
	}
	row.PublicNote = publicNote.String
	row.ReportURL = reportURL.String
	if expectedInput.Valid {
		v := int(expectedInput.Int64)
		row.ExpectedInputTokens = &v
	}
	if lastCheckedAt.Valid {
		t := lastCheckedAt.Time
		row.LastCheckedAt = &t
	}
	row.GroupPlatform = groupPlatform.String
	// 分组仍存在时以分组的真实名字为准：管理员可能改过分组名，
	// 而监控项里的 group_name 只是当初手填的字符串快照。
	if groupRealName.Valid && groupRealName.String != "" {
		row.GroupName = groupRealName.String
	}
	return &row, nil
}

// decodeChannelMonitorExtraModels 解析 extra_models（jsonb）。
// NULL / 空值都归一成空切片，避免调用方处理 nil。
func decodeChannelMonitorExtraModels(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	var models []string
	if err := json.Unmarshal(raw, &models); err != nil {
		return nil, fmt.Errorf("decode extra_models: %w", err)
	}
	if models == nil {
		return []string{}, nil
	}
	return models, nil
}

// channelMonitorHallTimelineQuery 取窗口内的全部探测记录，**不做聚合**。
//
// 曲线要求「一次探测一个点」：桶内平均会把一次真实故障抹进周围的正常样本里，
// 而这页存在的意义就是让人看见故障。传输成本实测可接受——7d 全量点、21 个监控
// 约 6.4MB，线上 Caddy 开了 zstd+gzip6，压缩比 18x，过网 352KB。
//
// ROW_NUMBER 只是防炸上限（见 service.hallTimelineMaxPoints）：
// 默认 300s 间隔下 7d 才 2016 点，取不满；探测间隔被设到下限 15s 时才截断，
// 且保留最近的那批（ORDER BY checked_at DESC 排名）。
//
// 索引：与上游 ListRecentHistoryForMonitors 同形，
// 走 (monitor_id, model, checked_at DESC)。加了时间范围后规划器有可能改选
// checked_at 单列索引再 hash join，两条路都可接受——没有聚合就没有排序落盘，
// 实测 7d/21 监控（54 万行表）在 100ms 量级。
//
// 不含 message 字段，减少响应体。
const channelMonitorHallTimelineQuery = `
	WITH targets AS (
	    SELECT unnest($1::bigint[]) AS monitor_id,
	           unnest($2::text[])   AS model
	),
	ranked AS (
	    SELECT h.monitor_id,
	           h.status,
	           h.latency_ms,
	           h.ping_latency_ms,
	           h.checked_at,
	           h.ttft_ms,
	           h.input_tokens,
	           h.cached_input_tokens,
	           h.output_tokens,
	           ROW_NUMBER() OVER (PARTITION BY h.monitor_id ORDER BY h.checked_at DESC) AS rn
	    FROM channel_monitor_histories h
	    JOIN targets t
	      ON t.monitor_id = h.monitor_id AND t.model = h.model
	    WHERE h.checked_at >= $3 AND h.checked_at < $4
	)
	SELECT monitor_id, status, latency_ms, ping_latency_ms, checked_at,
	       ttft_ms, input_tokens, cached_input_tokens, output_tokens
	FROM ranked
	WHERE rn <= $5
	ORDER BY monitor_id, checked_at
`

// ListHistoryInWindowForMonitors 见 service.ChannelMonitorRepository 接口说明。
func (r *channelMonitorRepository) ListHistoryInWindowForMonitors(
	ctx context.Context,
	ids []int64,
	primaryModels map[int64]string,
	start, end time.Time,
	perMonitorLimit int,
) (map[int64][]*service.ChannelMonitorHistoryEntry, error) {
	out := make(map[int64][]*service.ChannelMonitorHistoryEntry, len(ids))
	pairIDs, pairModels := buildMonitorModelPairs(ids, primaryModels)
	if len(pairIDs) == 0 {
		return out, nil
	}
	// 窗口非法时返回空而不是报错：调用方拿到空曲线会显示占位符，
	// 比整页 500 合理。正常路径下窗口由 ParseFilter 产出，不会走到这里。
	if !end.After(start) || perMonitorLimit <= 0 {
		return out, nil
	}

	rows, err := r.db.QueryContext(
		ctx, channelMonitorHallTimelineQuery,
		pq.Array(pairIDs), pq.Array(pairModels), start.UTC(), end.UTC(), perMonitorLimit,
	)
	if err != nil {
		return nil, fmt.Errorf("query hall timeline: %w", err)
	}
	defer func() { _ = rows.Close() }()

	for rows.Next() {
		var monitorID int64
		entry := &service.ChannelMonitorHistoryEntry{}
		var latency, ping, ttft, inputTokens, cachedInputTokens, outputTokens sql.NullInt64
		if err := rows.Scan(
			&monitorID, &entry.Status, &latency, &ping, &entry.CheckedAt,
			&ttft, &inputTokens, &cachedInputTokens, &outputTokens,
		); err != nil {
			return nil, fmt.Errorf("scan hall timeline row: %w", err)
		}
		assignNullInt(&entry.LatencyMs, latency)
		assignNullInt(&entry.PingLatencyMs, ping)
		assignNullInt(&entry.TTFTMs, ttft)
		assignNullInt(&entry.InputTokens, inputTokens)
		assignNullInt(&entry.CachedInputTokens, cachedInputTokens)
		assignNullInt(&entry.OutputTokens, outputTokens)
		out[monitorID] = append(out[monitorID], entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

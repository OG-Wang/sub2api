package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
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

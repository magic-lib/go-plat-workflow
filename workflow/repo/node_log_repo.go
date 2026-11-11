package repo

import (
	"context"
	"fmt"
	"github.com/magic-lib/go-plat-utils/id-generator/id"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// NodeLogRepo node 运行日志仓储，实现 workflow.NodeLogStore 接口。
type NodeLogRepo struct {
	db *gorm.DB
}

// NewNodeLogRepo 创建 node 运行日志仓储实例。
func NewNodeLogRepo(db *gorm.DB) *NodeLogRepo {
	return &NodeLogRepo{db: db}
}

// Create 创建一条 node 运行日志。
func (r *NodeLogRepo) Create(ctx context.Context, def *workflow.NodeLogDef) error {
	var m models.NodeLogModel
	m.FromDef(def)
	return r.db.WithContext(ctx).Create(&m).Error
}

// ListByNode 列出指定 node 的运行日志（按时间倒序），支持分页。
func (r *NodeLogRepo) ListByNode(ctx context.Context, project, nodeID string, limit, offset int) ([]*workflow.NodeLogDef, int64, error) {
	query := r.db.WithContext(ctx).Model(&models.NodeLogModel{})
	query = query.Where("project = ? AND node_id = ?", project, nodeID)
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var modelsList []models.NodeLogModel
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&modelsList).Error; err != nil {
		return nil, 0, err
	}
	defs := make([]*workflow.NodeLogDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, total, nil
}

// StatsByDay 按 node + 天聚合统计每个 node 每天的访问量（总条数）与错误量（error_msg 非空条数）。
// days 表示统计最近 N 天（基于 created_at）；env 为空表示不限定环境；nodeID 为空表示统计全部 node。
// 返回结果按日期升序、node_id 升序排序，便于前端按日期横轴、node 分系列绘制。
func (r *NodeLogRepo) StatsByDay(ctx context.Context, project, env, nodeID string, days int) ([]workflow.NodeLogDayStat, error) {
	if days <= 0 {
		days = 7
	}
	if days > 365 {
		days = 365
	}
	since := StatsSince(days)

	type row struct {
		NodeID   string `gorm:"column:node_id"`
		NodeName string `gorm:"column:node_name"`
		Date     string `gorm:"column:date"`
		Total    int64  `gorm:"column:total"`
		Errors   int64  `gorm:"column:errors"`
	}
	var rows []row
	query := r.db.WithContext(ctx).Model(&models.NodeLogModel{}).
		// DATE_FORMAT 返回字符串：直接用 DATE() 会被驱动解析成 time，序列化后变成 2006-01-02T00:00:00+02:00
		Select("node_id, node_name, DATE_FORMAT(created_at, '%Y-%m-%d') as date, COUNT(*) as total, "+
			"SUM(CASE WHEN error_msg <> '' THEN 1 ELSE 0 END) as errors").
		// 按 ts（有索引，与 created_at 同一时刻写入）过滤，created_at 无索引
		Where("project = ? AND ts >= ?", project, since.Unix())
	if env != "" {
		query = query.Where("env = ?", env)
	}
	query = whereNodeTemplate(query, nodeID)
	if err := query.Group("node_id, node_name, DATE_FORMAT(created_at, '%Y-%m-%d')").
		Order("date ASC, node_id ASC").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	stats := make([]workflow.NodeLogDayStat, 0, len(rows))
	for _, rw := range rows {
		stats = append(stats, workflow.NodeLogDayStat{
			NodeID:   rw.NodeID,
			NodeName: rw.NodeName,
			Date:     rw.Date,
			Total:    rw.Total,
			Errors:   rw.Errors,
		})
	}
	return stats, nil
}

// StatsSince 统计起点：最近 days 天（含今天）的本地零点。
func StatsSince(days int) time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -(days - 1))
}

// whereNodeTemplate 按节点过滤：日志里的 node_id 是「模板ID__实例后缀」，
// 传入模板 ID（不含 __）时匹配该模板的所有实例，传入完整实例 ID 时精确匹配。
func whereNodeTemplate(query *gorm.DB, nodeID string) *gorm.DB {
	if nodeID == "" {
		return query
	}
	if strings.Contains(nodeID, "__") {
		return query.Where("node_id = ?", nodeID)
	}
	return query.Where("(node_id = ? OR node_id LIKE ?)", nodeID, escapeLike(nodeID)+`\_\_%`)
}

// escapeLike 转义 LIKE 通配符。
func escapeLike(s string) string {
	return strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(s)
}

// ExecBuckets 按执行聚合 node 日志后，再按（根链、发布版本、耗时分桶）汇总执行次数、失败次数与耗时。
// 一次执行 = 同一 trace_id + 根链 + 10 分钟窗口（见 workflow.StatsExecWindow）。
// ts 为节点结束时间（秒），开始时间取 ts*1000 - duration_ms。未带 trace_id 的日志不计入。
// 只返回分桶汇总而不是逐次执行明细，避免大流量项目把几十万行搬到应用层。
func (r *NodeLogRepo) ExecBuckets(ctx context.Context, project, env string, since time.Time) ([]workflow.StatsExecBucketRow, error) {
	inner := r.db.WithContext(ctx).Model(&models.NodeLogModel{}).
		Select("root_chain_id, MAX(root_chain_release_id) as release_id, "+
			"MAX(ts*1000) - MIN(ts*1000 - duration_ms) as dur, MAX(ts*1000) as end_ms, "+
			"SUM(CASE WHEN error_msg <> '' THEN 1 ELSE 0 END) as errors").
		Where("project = ? AND ts >= ? AND trace_id <> ''", project, since.Unix())
	if env != "" {
		inner = inner.Where("env = ?", env)
	}
	inner = inner.Group(fmt.Sprintf("root_chain_id, trace_id, ts DIV %d", workflow.StatsExecWindow))
	var rows []workflow.StatsExecBucketRow
	err := r.db.WithContext(ctx).Table("(?) as t", inner).
		Select("root_chain_id, release_id, FLOOR(LN(GREATEST(dur, 0) + 1) * 10) as bucket, COUNT(*) as count, " +
			"SUM(CASE WHEN errors > 0 THEN 1 ELSE 0 END) as failed, SUM(GREATEST(dur, 0)) as sum_ms, MAX(end_ms) as last_ms").
		Group("root_chain_id, release_id, bucket").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// NodeTotals 统计区间内 node 执行总次数与报错次数。
func (r *NodeLogRepo) NodeTotals(ctx context.Context, project, env string, since time.Time) (total, errors int64, err error) {
	var res struct {
		Total  int64 `gorm:"column:total"`
		Errors int64 `gorm:"column:errors"`
	}
	query := r.db.WithContext(ctx).Model(&models.NodeLogModel{}).
		Select("COUNT(*) as total, COALESCE(SUM(CASE WHEN error_msg <> '' THEN 1 ELSE 0 END), 0) as errors").
		Where("project = ? AND ts >= ?", project, since.Unix())
	if env != "" {
		query = query.Where("env = ?", env)
	}
	if err = query.Scan(&res).Error; err != nil {
		return 0, 0, err
	}
	return res.Total, res.Errors, nil
}

// ErrorGroups 按（节点模板、节点名、错误信息前 300 字符）聚合报错日志，按次数倒序最多返回 limit 组。
// 错误信息里常带 task id 等变量，调用方还需归一化后再合并。
func (r *NodeLogRepo) ErrorGroups(ctx context.Context, project, env string, since time.Time, limit int) ([]workflow.StatsErrorRow, error) {
	if limit <= 0 {
		limit = 500
	}
	var rows []workflow.StatsErrorRow
	query := r.db.WithContext(ctx).Model(&models.NodeLogModel{}).
		Select("SUBSTRING_INDEX(node_id, '__', 1) as node_id, node_name, LEFT(error_msg, 300) as error_msg, "+
			"COUNT(*) as count, MAX(ts) as last_ts, MAX(trace_id) as trace_id").
		Where("project = ? AND ts >= ? AND error_msg <> ''", project, since.Unix())
	if env != "" {
		query = query.Where("env = ?", env)
	}
	if err := query.Group("SUBSTRING_INDEX(node_id, '__', 1), node_name, LEFT(error_msg, 300)").
		Order("count DESC").Limit(limit).Scan(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

// ListByFilter 按条件全局查询 node 运行日志（按时间倒序），支持分页。
// 支持 level/node_id/env/trace_id 精确匹配，node_name/keyword 模糊匹配。
func (r *NodeLogRepo) ListByFilter(ctx context.Context, project string, f *workflow.NodeLogFilter) ([]*workflow.NodeLogDef, int64, error) {
	if f == nil {
		f = &workflow.NodeLogFilter{}
	}
	query := r.db.WithContext(ctx).Model(&models.NodeLogModel{})
	query = query.Where("project = ?", project)
	if f.Level != "" {
		query = query.Where("level = ?", f.Level)
	}
	if f.NodeID != "" {
		query = query.Where("node_id = ?", f.NodeID)
	}
	if f.NodeName != "" {
		query = query.Where("node_name LIKE ?", "%"+f.NodeName+"%")
	}
	if f.Env != "" {
		query = query.Where("env = ?", f.Env)
	}
	if f.TraceID != "" {
		query = query.Where("trace_id = ?", id.GetUUID(f.TraceID))
	}
	if f.RootChainID != "" {
		query = query.Where("root_chain_id = ?", f.RootChainID)
	}
	if f.ChainID != "" {
		query = query.Where("chain_id = ?", f.ChainID)
	}
	if f.Keyword != "" {
		kw := "%" + f.Keyword + "%"
		query = query.Where("payload LIKE ? OR result LIKE ? OR error_msg LIKE ?", kw, kw, kw)
	}
	var total int64
	if err := query.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	limit := f.Limit
	offset := f.Offset
	if limit <= 0 || limit > 1000 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	var modelsList []models.NodeLogModel
	if err := query.Order("id DESC").Limit(limit).Offset(offset).Find(&modelsList).Error; err != nil {
		return nil, 0, err
	}
	defs := make([]*workflow.NodeLogDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToDef())
	}
	return defs, total, nil
}

// DeleteOlderThan 删除 created_at 早于 before 的运行日志（用于按保留天数清理）。
func (r *NodeLogRepo) DeleteOlderThan(ctx context.Context, before time.Time) (int64, error) {
	result := r.db.WithContext(ctx).Where("created_at < ?", before).Delete(&models.NodeLogModel{})
	return result.RowsAffected, result.Error
}

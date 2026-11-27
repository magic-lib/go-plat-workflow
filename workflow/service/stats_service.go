package service

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/repo"
)

const (
	// statsCacheTTL 统计概览缓存时长：聚合查询要扫描区间内全部 node 日志，短时间内重复刷新直接用缓存
	statsCacheTTL = time.Minute
	// statsErrorTopN 错误排行返回条数
	statsErrorTopN = 20
	// statsErrorScanLimit 参与归一化合并的错误分组上限
	statsErrorScanLimit = 500
	// statsMaxDays 统计概览最多支持的天数
	statsMaxDays = 90
)

// statsCacheEntry 统计概览缓存项（不含心跳等实时数据）。
type statsCacheEntry struct {
	at time.Time
	ov *workflow.StatsOverview
}

// statsCache 统计概览缓存，key = project|env|days。
var statsCache sync.Map

// ActivityOnlineFunc 判断某 activity 当前是否有心跳（由 web 层基于收集器提供）。
type ActivityOnlineFunc func(actNamespace, actName string) bool

// StatsOverview 统计页概览：汇总卡片、根链执行统计（按发布版本拆分）、错误排行。
// env 为空表示不限定环境；days 为最近 N 天（1~90）；refresh 为 true 时跳过缓存。online 为 nil 时不统计 activity 在线数。
func (s *WorkflowService) StatsOverview(ctx context.Context, project, env string, days int, refresh bool, online ActivityOnlineFunc) (*workflow.StatsOverview, error) {
	if days <= 0 {
		days = 7
	}
	if days > statsMaxDays {
		days = statsMaxDays
	}
	base, err := s.statsOverviewCached(ctx, project, env, days, refresh)
	if err != nil {
		return nil, err
	}
	// 复制一份再填实时数据，避免改到缓存里的对象
	ov := *base
	summary := *base.Summary
	ov.Summary = &summary
	if online != nil {
		refs, err := s.ListPublishedActivities(ctx, project)
		if err != nil {
			return nil, fmt.Errorf("list published activities: %w", err)
		}
		published, on := len(refs), 0
		for _, ref := range refs {
			if online(ref.ActNamespace, ref.ActName) {
				on++
			}
		}
		summary.ActivityPublished = &published
		summary.ActivityOnline = &on
	}
	return &ov, nil
}

// statsOverviewCached 查库计算统计概览，结果缓存 statsCacheTTL。
func (s *WorkflowService) statsOverviewCached(ctx context.Context, project, env string, days int, refresh bool) (*workflow.StatsOverview, error) {
	key := project + "|" + env + "|" + strconv.Itoa(days)
	if v, ok := statsCache.Load(key); ok && !refresh {
		if e := v.(*statsCacheEntry); time.Since(e.at) < statsCacheTTL {
			return e.ov, nil
		}
	}
	since := repo.StatsSince(days)
	chains, err := s.rootChainRepo.List(ctx, project)
	if err != nil {
		return nil, fmt.Errorf("list root chains: %w", err)
	}
	names := make(map[string]string, len(chains))
	for _, c := range chains {
		names[c.ChainID] = c.Name
	}
	// 三个聚合各自扫描区间内的 node 日志，并发执行缩短等待
	var (
		buckets               []workflow.StatsExecBucketRow
		errRows               []workflow.StatsErrorRow
		nodeTotal, nodeErrors int64
	)
	var (
		wg                          sync.WaitGroup
		bucketErr, totalErr, grpErr error
	)
	wg.Add(3)
	go func() {
		defer wg.Done()
		buckets, bucketErr = s.nodeLogRepo.ExecBuckets(ctx, project, env, since)
	}()
	go func() {
		defer wg.Done()
		nodeTotal, nodeErrors, totalErr = s.nodeLogRepo.NodeTotals(ctx, project, env, since)
	}()
	go func() {
		defer wg.Done()
		errRows, grpErr = s.nodeLogRepo.ErrorGroups(ctx, project, env, since, statsErrorScanLimit)
	}()
	wg.Wait()
	if bucketErr != nil {
		return nil, fmt.Errorf("exec buckets: %w", bucketErr)
	}
	if totalErr != nil {
		return nil, fmt.Errorf("node totals: %w", totalErr)
	}
	if grpErr != nil {
		return nil, fmt.Errorf("error groups: %w", grpErr)
	}
	summary, rootStats := buildRootChainStats(buckets, names)
	summary.NodeTotal = nodeTotal
	summary.NodeErrors = nodeErrors
	ov := &workflow.StatsOverview{
		Env:        env,
		Days:       days,
		Summary:    summary,
		RootChains: rootStats,
		Errors:     mergeErrorStats(errRows, statsErrorTopN),
	}
	statsCache.Store(key, &statsCacheEntry{at: time.Now(), ov: ov})
	return ov, nil
}

// execAcc 执行统计累加器：基于耗时分桶汇总行累加。
type execAcc struct {
	count  int64
	failed int64
	sumMs  int64
	lastAt int64
	// hist 耗时分桶 → 执行次数
	hist map[int]int64
}

func (a *execAcc) add(r workflow.StatsExecBucketRow) {
	if a.hist == nil {
		a.hist = map[int]int64{}
	}
	a.count += r.Count
	a.failed += r.Failed
	a.sumMs += r.SumMs
	a.hist[r.Bucket] += r.Count
	if r.LastMs > a.lastAt {
		a.lastAt = r.LastMs
	}
}

func (a *execAcc) stat() workflow.ExecStat {
	st := workflow.ExecStat{Executions: a.count, Failed: a.failed, LastAt: a.lastAt}
	if a.count == 0 {
		return st
	}
	st.FailRate = float64(a.failed) / float64(a.count)
	st.AvgMs = a.sumMs / a.count
	st.P95Ms = histPercentile(a.hist, a.count, 0.95)
	return st
}

// histPercentile 由耗时分桶直方图估算分位数：取累计次数首次达到 p 的分桶上界。
// 分桶为 FLOOR(LN(ms+1)*10)，上界 = e^((b+1)/10) - 1，相对误差约 10%。
func histPercentile(hist map[int]int64, total int64, p float64) int64 {
	if total <= 0 || len(hist) == 0 {
		return 0
	}
	keys := make([]int, 0, len(hist))
	for b := range hist {
		keys = append(keys, b)
	}
	sort.Ints(keys)
	target := int64(math.Ceil(p * float64(total)))
	var cum int64
	for _, b := range keys {
		cum += hist[b]
		if cum >= target {
			return bucketUpperMs(b)
		}
	}
	return bucketUpperMs(keys[len(keys)-1])
}

// bucketUpperMs 耗时分桶的上界（毫秒）。
func bucketUpperMs(b int) int64 {
	return int64(math.Round(math.Exp(float64(b+1)/10) - 1))
}

// releaseVersion 从发布标识（R000005@3）解析版本号，草稿或格式不对返回 0。
func releaseVersion(releaseID string) int {
	i := strings.LastIndex(releaseID, "@")
	if i < 0 {
		return 0
	}
	v, err := strconv.Atoi(releaseID[i+1:])
	if err != nil {
		return 0
	}
	return v
}

// buildRootChainStats 汇总执行分桶：只统计 names 中存在的根链（节点单测、已删除根链的执行不计入）。
// 根链按执行次数倒序，版本按版本号倒序、草稿排最后。
func buildRootChainStats(rows []workflow.StatsExecBucketRow, names map[string]string) (*workflow.StatsSummary, []*workflow.RootChainStat) {
	all := &execAcc{}
	chainAcc := map[string]*execAcc{}
	verAcc := map[string]map[string]*execAcc{}
	for _, t := range rows {
		if _, ok := names[t.RootChainID]; !ok {
			continue
		}
		all.add(t)
		if chainAcc[t.RootChainID] == nil {
			chainAcc[t.RootChainID] = &execAcc{}
			verAcc[t.RootChainID] = map[string]*execAcc{}
		}
		chainAcc[t.RootChainID].add(t)
		va := verAcc[t.RootChainID][t.ReleaseID]
		if va == nil {
			va = &execAcc{}
			verAcc[t.RootChainID][t.ReleaseID] = va
		}
		va.add(t)
	}

	allStat := all.stat()
	summary := &workflow.StatsSummary{
		Executions:       allStat.Executions,
		FailedExecutions: allStat.Failed,
		AvgMs:            allStat.AvgMs,
		P95Ms:            allStat.P95Ms,
	}
	if allStat.Executions > 0 {
		summary.SuccessRate = 1 - allStat.FailRate
	}

	out := make([]*workflow.RootChainStat, 0, len(chainAcc))
	for chainID, acc := range chainAcc {
		rs := &workflow.RootChainStat{ChainID: chainID, Name: names[chainID], ExecStat: acc.stat()}
		for releaseID, va := range verAcc[chainID] {
			rs.Versions = append(rs.Versions, &workflow.RootChainVersionStat{
				ReleaseID: releaseID,
				Version:   releaseVersion(releaseID),
				ExecStat:  va.stat(),
			})
		}
		sort.Slice(rs.Versions, func(i, j int) bool {
			vi, vj := rs.Versions[i].Version, rs.Versions[j].Version
			if (vi == 0) != (vj == 0) {
				return vj == 0
			}
			if vi != vj {
				return vi > vj
			}
			return rs.Versions[i].ReleaseID < rs.Versions[j].ReleaseID
		})
		out = append(out, rs)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Executions != out[j].Executions {
			return out[i].Executions > out[j].Executions
		}
		return out[i].ChainID < out[j].ChainID
	})
	return summary, out
}

var (
	uuidRe    = regexp.MustCompile(`(?i)[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}`)
	hexIDRe   = regexp.MustCompile(`(?i)\b[0-9a-f]{24,}\b`)
	longNumRe = regexp.MustCompile(`\d{6,}`)
)

// normalizeErrorMsg 把错误信息里的 task id、trace id、长数字等变量替换成占位符，便于同类错误合并计数。
func normalizeErrorMsg(msg string) string {
	msg = uuidRe.ReplaceAllString(msg, "<id>")
	msg = hexIDRe.ReplaceAllString(msg, "<id>")
	msg = longNumRe.ReplaceAllString(msg, "<n>")
	msg = strings.TrimSpace(msg)
	if r := []rune(msg); len(r) > 200 {
		msg = string(r[:200]) + "…"
	}
	return msg
}

// mergeErrorStats 按（节点模板、节点名、归一化错误信息）合并，按次数倒序取前 topN。
func mergeErrorStats(rows []workflow.StatsErrorRow, topN int) []*workflow.ErrorStat {
	merged := map[string]*workflow.ErrorStat{}
	for _, r := range rows {
		msg := normalizeErrorMsg(r.ErrorMsg)
		key := r.NodeID + "\x00" + r.NodeName + "\x00" + msg
		e := merged[key]
		if e == nil {
			e = &workflow.ErrorStat{NodeID: r.NodeID, NodeName: r.NodeName, Message: msg}
			merged[key] = e
		}
		e.Count += r.Count
		if last := r.LastTs * 1000; last > e.LastAt {
			e.LastAt = last
			e.TraceID = r.TraceID
		}
	}
	out := make([]*workflow.ErrorStat, 0, len(merged))
	for _, e := range merged {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].LastAt > out[j].LastAt
	})
	if topN > 0 && len(out) > topN {
		out = out[:topN]
	}
	return out
}

package service

import (
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// 启动迁移：表结构最终态已全部写进 model 的 gorm 标签，AutoMigrate 可一次建好新库。
// 这里只保留两类必须单独处理的步骤，都是先检查再动手，已是最终态的库只做几次元数据查询：
//   - 老库遗留清理：早期由显式 SQL 建的、与 model 声明重复或口径不一致的索引；
//   - chain_key 全局唯一索引：老库可能存在跨项目重名，换索引前要检查，不能写在标签里让 AutoMigrate 直接建。
//
// 已移除的历史步骤（model 已是最终态，AutoMigrate 自行处理或已无必要）：
//   - wf_activity_logs.result MODIFY 为 text；
//   - wf_node_logs.event_id / relation_type 每次启动删除并重建前缀索引、MODIFY 为 varchar(500)：
//     改为在 model 中声明同名前缀索引（index:event_id,length:191），老库按名字识别为已存在，不再重建。

const (
	chainKeyUniqueIndex       = "uk_chain_key"         // 最终态：chain_key 单列全局唯一
	legacyChainKeyUniqueIndex = "uk_project_chain_key" // 老库：(project, chain_key)，或早期标签写错建成的单列
	legacyTestRecordTraceIdx  = "idx_trace_id"         // 老库：与 model 声明的 trace_id 索引重复
	maxDupChainKeysInLog      = 20
)

func migrateSchemaLegacy(db *gorm.DB) error {
	if err := dropIndexIfExists(db, &models.NodeTestRecordModel{}, legacyTestRecordTraceIdx); err != nil {
		return err
	}
	return ensureChainKeyUniqueIndex(db)
}

// dropIndexIfExists 索引存在时删除（按索引名判断）。
func dropIndexIfExists(db *gorm.DB, model any, name string) error {
	m := db.Migrator()
	if !m.HasIndex(model, name) {
		return nil
	}
	if err := m.DropIndex(model, name); err != nil {
		return fmt.Errorf("drop index %s: %w", name, err)
	}
	log.Info().Str("index", name).Msg("schema migrate: dropped legacy index")
	return nil
}

// ensureChainKeyUniqueIndex 保证 wf_root_chains.chain_key 有全局唯一索引 uk_chain_key：
//  1. 回填空 chain_key（按 id 生成，幂等），避免空值重复导致建唯一索引失败；
//  2. 已有 uk_chain_key：只清理遗留的旧索引；
//  3. 没有时先查重名（含软删除的行，唯一索引对它们同样生效），有重名则保留旧索引并告警，不阻断启动；
//  4. 无重名：先建新索引再删旧索引，过程中不会出现没有唯一约束的空窗。
func ensureChainKeyUniqueIndex(db *gorm.DB) error {
	if err := db.Exec(
		"UPDATE wf_root_chains SET chain_key = CONCAT('R', LPAD(id, 6, '0')) WHERE chain_key IS NULL OR chain_key = ''",
	).Error; err != nil {
		return fmt.Errorf("backfill chain_key: %w", err)
	}
	model := &models.RootChainModel{}
	if db.Migrator().HasIndex(model, chainKeyUniqueIndex) {
		return dropIndexIfExists(db, model, legacyChainKeyUniqueIndex)
	}

	dups, err := duplicateChainKeys(db)
	if err != nil {
		return err
	}
	if len(dups) > 0 {
		log.Warn().Strs("chain_keys", dups).
			Msg("schema migrate: chain_key 存在重名，暂不建立全局唯一索引 uk_chain_key，请先处理重名数据后重启")
		return nil
	}
	if err := db.Exec("CREATE UNIQUE INDEX " + chainKeyUniqueIndex + " ON wf_root_chains (chain_key)").Error; err != nil {
		return fmt.Errorf("create index %s: %w", chainKeyUniqueIndex, err)
	}
	log.Info().Str("index", chainKeyUniqueIndex).Msg("schema migrate: created chain_key global unique index")
	return dropIndexIfExists(db, model, legacyChainKeyUniqueIndex)
}

// duplicateChainKeys 返回出现多次的 chain_key（最多 maxDupChainKeysInLog 个），格式 key(次数)。
func duplicateChainKeys(db *gorm.DB) ([]string, error) {
	var rows []struct {
		ChainKey string
		Cnt      int64
	}
	if err := db.Raw(
		"SELECT chain_key, COUNT(*) AS cnt FROM wf_root_chains GROUP BY chain_key HAVING COUNT(*) > 1 ORDER BY cnt DESC LIMIT ?",
		maxDupChainKeysInLog,
	).Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("check duplicate chain_key: %w", err)
	}
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s(%d)", strings.TrimSpace(r.ChainKey), r.Cnt))
	}
	return out, nil
}

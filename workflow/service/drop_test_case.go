package service

import (
	"fmt"

	"github.com/rs/zerolog/log"
	"gorm.io/gorm"
)

// legacyTestCaseTable 已下线的测试用例表（编排页「测试用例」功能早已隐藏，接口与代码已删除）。
const legacyTestCaseTable = "wf_test_cases"

// dropUnusedTestCaseTable 启动迁移时清理已下线的 wf_test_cases 表：
// 表不存在直接跳过；表里还有数据只告警不删，避免误删；空表才删除。
func dropUnusedTestCaseTable(db *gorm.DB) error {
	m := db.Migrator()
	return dropTableIfEmpty(legacyTestCaseTable,
		func() bool { return m.HasTable(legacyTestCaseTable) },
		func() (int64, error) {
			var n int64
			err := db.Table(legacyTestCaseTable).Count(&n).Error
			return n, err
		},
		func() error { return m.DropTable(legacyTestCaseTable) },
	)
}

// dropTableIfEmpty 表存在且为空时删除；有数据只告警跳过。拆出判断逻辑便于单测。
func dropTableIfEmpty(table string, has func() bool, count func() (int64, error), drop func() error) error {
	if !has() {
		return nil
	}
	n, err := count()
	if err != nil {
		return fmt.Errorf("count %s: %w", table, err)
	}
	if n > 0 {
		log.Warn().Str("table", table).Int64("rows", n).Msg("legacy table still has data, skip drop")
		return nil
	}
	if err := drop(); err != nil {
		return fmt.Errorf("drop %s: %w", table, err)
	}
	log.Info().Str("table", table).Msg("legacy empty table dropped")
	return nil
}

package repo

import (
	"context"
	"fmt"

	"gorm.io/gorm"

	"github.com/magic-lib/go-plat-workflow/workflow"
)

// formatBizID 按「前缀 + 至少 6 位自增 id」生成正式业务编号，如 N000012；超过 999999 自然变为 7 位。
func formatBizID(prefix string, id uint) string {
	return fmt.Sprintf("%s%06d", prefix, id)
}

// needAutoBizID 业务编号为空或为临时编号时，需要按自增 id 生成正式编号。
func needAutoBizID(id string) bool {
	return id == "" || workflow.IsTempBizID(id)
}

// bizIDCreate 「先插入、再回写」所需的模型信息。
type bizIDCreate struct {
	// model 待插入的模型指针，插入后由 gorm 回填自增主键
	model any
	// column 业务编号列名
	column string
	// prefix 业务编号前缀
	prefix string
	// tempID 插入时使用的临时编号（已写入 model）
	tempID string
	// id 插入后读取自增主键
	id func() uint
	// extra 回写时额外更新的列（如把 DSL 里的临时编号替换为正式编号），可为空
	extra func(finalID string) map[string]any
}

// createWithAutoBizID 在一个事务里先用临时编号插入，取得自增主键后回写正式业务编号，返回正式编号。
// 自增主键由数据库保证唯一，并发创建、删除后再创建都不会出现重复编号；任一步失败整体回滚，不残留临时编号。
func createWithAutoBizID(ctx context.Context, db *gorm.DB, c bizIDCreate) (string, error) {
	var finalID string
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(c.model).Error; err != nil {
			return err
		}
		id := c.id()
		if id == 0 {
			return fmt.Errorf("回写 %s：未取得自增主键", c.column)
		}
		finalID = formatBizID(c.prefix, id)
		updates := map[string]any{c.column: finalID}
		if c.extra != nil {
			for k, v := range c.extra(finalID) {
				updates[k] = v
			}
		}
		res := tx.Model(c.model).Where(c.column+" = ?", c.tempID).Updates(updates)
		if res.Error != nil {
			return fmt.Errorf("回写 %s=%s: %w", c.column, finalID, res.Error)
		}
		if res.RowsAffected != 1 {
			return fmt.Errorf("回写 %s=%s：影响行数 %d", c.column, finalID, res.RowsAffected)
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return finalID, nil
}

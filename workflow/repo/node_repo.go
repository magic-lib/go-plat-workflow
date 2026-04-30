package repo

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/magic-lib/go-plat-workflow/workflow"
	"github.com/magic-lib/go-plat-workflow/workflow/models"
)

// NodeRepo 节点仓储，实现 workflow.NodeStore 接口。
type NodeRepo struct {
	db *gorm.DB
}

// NewNodeRepo 创建节点仓储实例。
func NewNodeRepo(db *gorm.DB) *NodeRepo {
	return &NodeRepo{db: db}
}

// NextNodeID 生成下一个节点的自动 ID（如 N000005），基于 max(id)+1。
func (r *NodeRepo) NextNodeID(ctx context.Context) (string, error) {
	var next uint
	err := r.db.WithContext(ctx).Raw("SELECT COALESCE(MAX(id), 0) + 1 FROM wf_nodes").Scan(&next).Error
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("N%06d", next), nil
}

// Create 创建节点。
func (r *NodeRepo) Create(ctx context.Context, def *workflow.NodeDef) error {
	var m models.NodeModel
	m.FromNodeDef(def)
	return r.db.WithContext(ctx).Create(&m).Error
}

// BatchUpsert 批量 upsert 节点：project + node_id 冲突时更新全部字段，否则插入。
func (r *NodeRepo) BatchUpsert(ctx context.Context, defs []*workflow.NodeDef) error {
	if len(defs) == 0 {
		return nil
	}
	modelsList := make([]models.NodeModel, 0, len(defs))
	for _, def := range defs {
		var m models.NodeModel
		m.FromNodeDef(def)
		modelsList = append(modelsList, m)
	}
	return r.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "project"}, {Name: "node_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "type", "debug_mode", "configuration",
			"additional_info", "params", "outputs", "kind", "category", "description", "status", "version",
		}),
	}).Create(&modelsList).Error
}

// GetByID 按项目 + 节点 ID 查询。
func (r *NodeRepo) GetByID(ctx context.Context, project, nodeID string) (*workflow.NodeDef, error) {
	var m models.NodeModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND node_id = ? AND status = ?", project, nodeID, models.NodeStatusEnabled).
		First(&m).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, workflow.ErrNodeNotFound
		}
		return nil, err
	}
	return m.ToNodeDef(), nil
}

// ListByIDs 按项目 + 节点 ID 列表批量查询。
func (r *NodeRepo) ListByIDs(ctx context.Context, project string, nodeIDs []string) ([]*workflow.NodeDef, error) {
	if len(nodeIDs) == 0 {
		return nil, nil
	}
	var modelsList []models.NodeModel
	err := r.db.WithContext(ctx).
		Where("project = ? AND node_id IN ? AND status = ?", project, nodeIDs, models.NodeStatusEnabled).
		Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.NodeDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToNodeDef())
	}
	return defs, nil
}

// List 列出指定项目下的节点。onlyEnabled=true 时仅返回启用状态的节点（用于编排选择）；
// onlyEnabled=false 时返回全部（含禁用，用于管理列表展示）。
func (r *NodeRepo) List(ctx context.Context, project string, namespace string, onlyEnabled bool) ([]*workflow.NodeDef, error) {
	var modelsList []models.NodeModel
	query := r.db.WithContext(ctx).
		Where("project = ?", project)
	if onlyEnabled {
		query = query.Where("status = ?", models.NodeStatusEnabled)
	}
	if namespace != "" {
		query = query.Where("namespace = ?", namespace)
	}
	err := query.Find(&modelsList).Error
	if err != nil {
		return nil, err
	}
	defs := make([]*workflow.NodeDef, 0, len(modelsList))
	for i := range modelsList {
		defs = append(defs, modelsList[i].ToNodeDef())
	}
	return defs, nil
}

// Update 更新节点。
func (r *NodeRepo) Update(ctx context.Context, def *workflow.NodeDef) error {
	result := r.db.WithContext(ctx).
		Model(&models.NodeModel{}).
		Where("project = ? AND node_id = ?", def.Project, def.NodeID).
		Updates(map[string]interface{}{
			"name":            def.Name,
			"type":            def.Type,
			"debug_mode":      def.DebugMode,
			"namespace":       def.Namespace,
			"configuration":   string(def.Configuration),
			"additional_info": string(def.AdditionalInfo),
			"params":          string(def.Params),
			"outputs":         string(def.Outputs),
			"kind":            def.Kind,
			"category":        def.Category,
			"tags":            models.SerializeActivityTags(def.Tags),
			"description":     def.Description,
			"status":          def.Status,
			"version":         def.Version,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrNodeNotFound
	}
	return nil
}

// Delete 软删除节点（按 project + nodeID）。
func (r *NodeRepo) Delete(ctx context.Context, project, nodeID string) error {
	result := r.db.WithContext(ctx).
		Where("project = ? AND node_id = ?", project, nodeID).
		Delete(&models.NodeModel{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return workflow.ErrNodeNotFound
	}
	return nil
}

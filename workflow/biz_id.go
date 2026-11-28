package workflow

import (
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"strings"
	"time"
)

// TempBizIDPrefix 临时业务编号前缀。
//
// 节点（N）、子链（F）、测试用例（T）、测试记录（M/T）的业务编号统一「先插入、再回写」：
// 先用临时编号插入取得自增主键 id，再按「前缀 + %06d(id)」回写正式编号，由数据库自增保证唯一。
// 需要在入库前就用到编号的场景（如子链组装 DSL 时写入 ruleChain.id），先用 NewTempBizID 生成临时编号，
// 仓储层入库时识别该前缀并把临时编号替换为正式编号。
const TempBizIDPrefix = "__tmp_"

// NewTempBizID 生成临时业务编号：前缀 + 纳秒时间戳 + 随机串，保证全局不重复且不会与正式编号混淆。
func NewTempBizID() string {
	b := make([]byte, 6)
	if _, err := rand.Read(b); err != nil {
		// 随机源不可用时退化为仅用时间戳，仍满足唯一索引（同纳秒冲突概率极低，冲突时插入报错回滚）
		return TempBizIDPrefix + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	return TempBizIDPrefix + strconv.FormatInt(time.Now().UnixNano(), 36) + "_" + hex.EncodeToString(b)
}

// IsTempBizID 判断是否为临时业务编号。
func IsTempBizID(id string) bool {
	return strings.HasPrefix(id, TempBizIDPrefix)
}

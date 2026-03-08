// Input: database/sql/driver, encoding/json, github.com/lib/pq
// Output: StringArray (pq.StringArray 别名), JSON[T] 泛型类型 (GORM JSONB 字段序列化/反序列化)
// Role: 自定义数据库类型，为 GORM 提供 PostgreSQL text[] 和 JSONB 字段的 Go 映射
// 自指声明: 本文件更新后，必须同步校准头部注释，并向上冒泡更新所属目录的 README.md
package schema

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"

	"github.com/lib/pq"
)

// StringArray is a PostgreSQL text[] compatible type.
type StringArray = pq.StringArray

// JSON is a generic JSON type for GORM.
type JSON[T any] struct {
	Data  T
	Valid bool
}

func NewJSON[T any](data T) JSON[T] {
	return JSON[T]{Data: data, Valid: true}
}

func (j JSON[T]) Value() (driver.Value, error) {
	if !j.Valid {
		return nil, nil
	}
	return json.Marshal(j.Data)
}

func (j *JSON[T]) Scan(value interface{}) error {
	if value == nil {
		j.Valid = false
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("unsupported type: %T", value)
	}
	j.Valid = true
	return json.Unmarshal(bytes, &j.Data)
}

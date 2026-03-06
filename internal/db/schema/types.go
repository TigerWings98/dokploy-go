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

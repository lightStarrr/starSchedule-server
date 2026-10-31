// Package store 定义存储层通用错误。
package store

import "errors"

var (
	ErrNotFound = errors.New("记录不存在")
	ErrConflict = errors.New("记录冲突")
)

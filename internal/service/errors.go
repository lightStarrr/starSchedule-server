// Package service 承载业务逻辑，handler 只做参数校验与响应组装。
package service

import "errors"

var (
	ErrDeviceBanned       = errors.New("设备已被封禁")
	ErrTokenStale         = errors.New("token 已失效，请重新注册")
	ErrModelNotFound      = errors.New("模型不可用")
	ErrAINotConfigured    = errors.New("服务端尚未配置可用的 AI 模型")
	ErrInvalidCredentials = errors.New("用户名或密码错误")
)

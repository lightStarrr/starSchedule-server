// Package version 保存构建期注入的版本信息。
package version

// 由 -ldflags "-X .../version.Version=..." 注入。
var (
	Version   = "dev"
	Commit    = "none"
	BuildTime = "unknown"
)

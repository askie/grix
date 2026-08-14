// Package version 暴露构建时通过 -ldflags 注入的版本信息，供 /version 端点上报，
// 使线上 pod 能反查自己对应哪个 Git Tag。默认值用于本地 go run（未走发布构建）。
package version

var (
	Version   = "dev"
	Commit    = "unknown"
	BuildTime = "unknown"
)

type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildTime string `json:"build_time"`
}

func Get() Info {
	return Info{Version: Version, Commit: Commit, BuildTime: BuildTime}
}

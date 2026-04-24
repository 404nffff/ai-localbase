package main

import (
	"embed"
	"io/fs"
	"os"
)

//go:embed all:dist
var embeddedFrontend embed.FS

// frontendFS 返回前端静态文件系统。
// 如果 backend/dist/ 下有非 .gitkeep 的实际构建产物，使用 os.DirFS 读取磁盘（方便开发调试）；
// 否则使用 embed.FS（生产构建时内嵌到 exe 中）。
func frontendFS() fs.FS {
	entries, err := os.ReadDir("dist")
	if err == nil {
		for _, e := range entries {
			if e.Name() != ".gitkeep" {
				return os.DirFS("dist")
			}
		}
	}
	sub, _ := fs.Sub(embeddedFrontend, "dist")
	return sub
}

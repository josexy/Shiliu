package app

import (
	"os"
	"path/filepath"

	"github.com/egoist/mygo"

	"go-downloader/internal/appinfo"
)

// useLegacyDataDir runs before the single-instance lock and before PathUserData
// can create a new directory. Reuse existing data without copying or moving it.
func useLegacyDataDir() {
	root, err := mygo.App.Path(mygo.PathAppData)
	if err != nil {
		return
	}
	if dir := legacyDataDir(root, mygo.App.Name()); dir != "" {
		mygo.App.SetPath(mygo.PathUserData, dir)
	}
}

func legacyDataDir(root, name string) string {
	var legacy string
	switch name {
	case appinfo.Name:
		legacy = "Downloader"
	case appinfo.Name + " Dev":
		legacy = "Downloader Dev"
	case appinfo.Name + " Local":
		legacy = "go-downloader"
	default:
		return ""
	}
	// An existing new directory takes precedence, including an empty one.
	if _, err := os.Stat(filepath.Join(root, name)); !os.IsNotExist(err) {
		return ""
	}
	dir := filepath.Join(root, legacy)
	if info, err := os.Stat(dir); err == nil && info.IsDir() {
		return dir
	}
	return ""
}

func legacySecretService(dirName string) string {
	switch dirName {
	case "Downloader":
		return "com.example.downloader"
	case "Downloader Dev":
		return "com.example.downloader.dev"
	case "go-downloader":
		return "go-downloader"
	default:
		return ""
	}
}

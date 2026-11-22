// Package appinfo defines the application name, version and request identity without
// depending on the UI framework.
package appinfo

// Name is the display name shared by the window, tray and About page.
const Name = "拾流"

// Product is the ASCII name used in HTTP request identification.
const Product = "Shiliu"

// NormalizeVersion uses dev when no version was injected by the build tool.
func NormalizeVersion(version string) string {
	if version == "" {
		return "dev"
	}
	return version
}

// UserAgent identifies the app and its version to servers.
func UserAgent(version string) string {
	return Product + "/" + NormalizeVersion(version) + " (MyGo)"
}

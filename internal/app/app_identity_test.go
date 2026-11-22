package app

import (
	"os"
	"path/filepath"
	"testing"

	"go-downloader/internal/appinfo"
)

func TestLegacyDataIdentity(t *testing.T) {
	for _, tc := range []struct{ name, old, service string }{
		{appinfo.Name, "Downloader", "com.example.downloader"},
		{appinfo.Name + " Dev", "Downloader Dev", "com.example.downloader.dev"},
		{appinfo.Name + " Local", "go-downloader", "go-downloader"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if got := legacyDataDir(root, tc.name); got != "" {
				t.Fatalf("fresh install reused %q", got)
			}
			old := filepath.Join(root, tc.old)
			if err := os.Mkdir(old, 0o755); err != nil {
				t.Fatal(err)
			}
			tasks := filepath.Join(old, "tasks.json")
			if err := os.WriteFile(tasks, []byte("[]\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			if got := legacyDataDir(root, tc.name); got != old {
				t.Fatalf("legacy directory = %q, want %q", got, old)
			}
			if got := legacySecretService(filepath.Base(old)); got != tc.service {
				t.Fatalf("secret service = %q, want %q", got, tc.service)
			}
			if err := os.Mkdir(filepath.Join(root, tc.name), 0o755); err != nil {
				t.Fatal(err)
			}
			if got := legacyDataDir(root, tc.name); got != "" {
				t.Fatalf("existing new directory lost precedence to %q", got)
			}
			if data, err := os.ReadFile(tasks); err != nil || string(data) != "[]\n" {
				t.Fatalf("legacy records changed: %q, %v", data, err)
			}
		})
	}
}

func TestLegacyDataIsolation(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "Downloader"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{appinfo.Name + " Dev", appinfo.Name + " Local", "Native Check"} {
		if got := legacyDataDir(root, name); got != "" {
			t.Errorf("%q reused release data at %q", name, got)
		}
	}
	if got := legacySecretService(appinfo.Name); got != "" {
		t.Errorf("new install should use MyGo's default credential service, got %q", got)
	}
}

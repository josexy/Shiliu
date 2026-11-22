package rules_test

import (
	"go-downloader/internal/rules"
	"go-downloader/internal/settings"
	"path/filepath"
	"reflect"
	"testing"
)

func TestRuleMatchingAndValidation(t *testing.T) {
	base := t.TempDir()
	input := []settings.DirectoryRule{
		{ID: "ext", Enabled: true, Kind: "extension", Pattern: ".ZIP", Dir: filepath.Join(base, "archives")},
		{ID: "domain", Enabled: true, Kind: "domain", Pattern: "*.例子.测试.", Dir: filepath.Join(base, "domain")},
		{ID: "exact", Enabled: true, Kind: "domain", Pattern: "EXAMPLE.COM.", Dir: filepath.Join(base, "exact")},
	}
	list, err := settings.NormalizeDirectoryRules(input)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ url, name, want string }{
		{"https://sub.例子.测试:8080/file", "FILE.ZIP", "ext"},
		{"https://sub.xn--fsqu00a.xn--0zwm56d/file", "file", "domain"},
		{"https://例子.测试/file", "file", ""},
		{"https://evil-example.com/file", "file", ""},
		{"https://sub.example.com/file", "file", ""},
		{"https://EXAMPLE.COM.:80/file", "file", "exact"},
		{"https://[::1]:8080/file", "file", ""},
		{"https://example.net/a.tar.gz", "a.tar.gz", ""},
	} {
		_, id := rules.Select(list, tc.url, tc.name, base, "")
		if id != tc.want {
			t.Fatalf("%+v => %s", tc, id)
		}
	}
	list[0].Enabled = false
	if _, id := rules.Select(list, "https://sub.例子.测试/a.zip", "a.zip", base, ""); id != "domain" {
		t.Fatal(id)
	}
	if dir, id := rules.Select(list, "https://sub.例子.测试/a.zip", "a.zip", base, "override"); dir != "override" || id != "" {
		t.Fatal(dir, id)
	}
	for _, tc := range []struct{ kind, pattern, dir string }{
		{"extension", "tar.gz", base}, {"extension", "*", base}, {"domain", "example.com:80", base}, {"domain", "https://example.com", base},
		{"domain", "x/evil", base}, {"domain", "foo*bar.com", base}, {"domain", "a..com", base}, {"extension", "zip", "relative"},
	} {
		bad := append([]settings.DirectoryRule(nil), input...)
		bad[0].Kind, bad[0].Pattern, bad[0].Dir = tc.kind, tc.pattern, tc.dir
		before := append([]settings.DirectoryRule(nil), bad...)
		if _, err := settings.NormalizeDirectoryRules(bad); err == nil {
			t.Fatal("accepted invalid rule", tc)
		}
		if !reflect.DeepEqual(bad, before) {
			t.Fatal("validation mutated caller")
		}
	}
}

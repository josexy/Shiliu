package rules

import (
	"fmt"
	"golang.org/x/net/idna"
	"path/filepath"
	"strings"
	"unicode"
)

type Rule struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Kind    string `json:"kind"` // extension or domain
	Pattern string `json:"pattern"`
	Dir     string `json:"dir"`
}

func NormalizeHost(raw string) (string, error) {
	host, err := idna.Lookup.ToASCII(strings.TrimSuffix(strings.ToLower(strings.TrimSpace(raw)), "."))
	if err != nil || host == "" || strings.ContainsAny(host, "/:@?#[\\]*") {
		return "", fmt.Errorf("域名须为主机名，不能含协议、端口或路径")
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return "", fmt.Errorf("域名标签无效")
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return "", fmt.Errorf("域名含无效字符")
			}
		}
	}
	return host, nil
}

// Normalize returns a fresh slice only when every edit is valid.
func Normalize(input []Rule) ([]Rule, error) {
	result := append([]Rule(nil), input...)
	ids := map[string]bool{}
	for i := range result {
		r := &result[i]
		if r.ID == "" || ids[r.ID] {
			return nil, fmt.Errorf("第 %d 条规则的标识缺失或重复", i+1)
		}
		ids[r.ID] = true
		r.Pattern = strings.TrimSpace(r.Pattern)
		r.Dir = strings.TrimSpace(r.Dir)
		if !filepath.IsAbs(r.Dir) {
			return nil, fmt.Errorf("第 %d 条规则请选择绝对目标目录", i+1)
		}
		r.Dir = filepath.Clean(r.Dir)
		switch r.Kind {
		case "extension":
			r.Pattern = strings.ToLower(strings.TrimPrefix(r.Pattern, "."))
			if r.Pattern == "" {
				return nil, fmt.Errorf("第 %d 条规则缺少扩展名", i+1)
			}
			for _, c := range r.Pattern {
				if !unicode.IsLetter(c) && !unicode.IsDigit(c) && c != '-' && c != '_' {
					return nil, fmt.Errorf("扩展名仅填写最后后缀，例如 zip")
				}
			}
		case "domain":
			wild := strings.HasPrefix(r.Pattern, "*.")
			host, err := NormalizeHost(strings.TrimPrefix(r.Pattern, "*."))
			if err != nil {
				return nil, fmt.Errorf("第 %d 条规则：%w", i+1, err)
			}
			r.Pattern = host
			if wild {
				r.Pattern = "*." + host
			}
		default:
			return nil, fmt.Errorf("第 %d 条规则类型无效", i+1)
		}
	}
	return result, nil
}

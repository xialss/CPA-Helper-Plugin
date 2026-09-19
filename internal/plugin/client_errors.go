package plugin

import (
	"fmt"
	"net/http"
	"strings"

	"cpa-helper-plugin/internal/policy"
)

// disabledKeyMessage never uses a header identity that differs from CPA's principal.
func disabledKeyMessage(scope string, headers http.Header) string {
	for name, values := range headers {
		if !strings.EqualFold(name, "Authorization") && !strings.EqualFold(name, "X-Api-Key") && !strings.EqualFold(name, "X-Goog-Api-Key") {
			continue
		}
		for _, value := range values {
			if strings.EqualFold(name, "Authorization") {
				parts := strings.SplitN(value, " ", 2)
				if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") {
					value = parts[1]
				}
			}
			value = strings.TrimSpace(value)
			if value == "" || policy.CallerScope(value) != scope {
				continue
			}
			runes := []rune(value)
			masked := "***"
			if len(runes) > 10 {
				masked = string(runes[:6]) + "..." + string(runes[len(runes)-4:])
			}
			return fmt.Sprintf("当前 API Key: '%s' 已被禁用", masked)
		}
	}
	return "当前 API Key 已被禁用（宿主未提供可核验的原始 Key，无法显示脱敏值）"
}

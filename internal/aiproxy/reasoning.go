package aiproxy

// ClampReasoningEffort 把服务端配置的默认档位收敛到某个模型实际支持的档位。
//
// 后台的默认强度是全局一个值，而单个模型的 reasoning_efforts 可能只是子集（例如只有 low/medium）。
// 直接下发越界值会被上游拒掉，静默丢弃又会让「接口说默认 high、其实什么都没发」——两种都不好。
// 这里按 minimal < low < medium < high 的顺序取索引距离最近的档位，距离相同取较低档（省 token）。
//
// 空字符串或模型没有声明档位时返回空，表示不下发该字段。
func ClampReasoningEffort(level string, efforts []string) string {
	if level == "" || len(efforts) == 0 {
		return ""
	}
	if indexOf(level) < 0 {
		return ""
	}
	if containsReasoningEffort(efforts, level) {
		return level
	}

	target := indexOf(level)
	best := ""
	bestDistance := -1
	for _, candidate := range efforts {
		index := indexOf(candidate)
		if index < 0 {
			continue
		}
		distance := index - target
		if distance < 0 {
			distance = -distance
		}
		// 同距离时保留较低档：distance 相等但候选更靠前才替换
		if bestDistance < 0 || distance < bestDistance ||
			(distance == bestDistance && index < indexOf(best)) {
			best = candidate
			bestDistance = distance
		}
	}
	return best
}

func containsReasoningEffort(efforts []string, value string) bool {
	for _, effort := range efforts {
		if effort == value {
			return true
		}
	}
	return false
}

// indexOf 返回档位在固定顺序里的下标，未知值返回 -1。
func indexOf(level string) int {
	for i, v := range ReasoningEfforts {
		if v == level {
			return i
		}
	}
	return -1
}

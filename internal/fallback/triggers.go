package fallback

const (
	TriggerTimeout          = "timeout"
	TriggerBackendUnhealthy = "backend_unhealthy"
	TriggerUpstream4xx      = "upstream_4xx"
	TriggerUpstream5xx      = "upstream_5xx"
	TriggerBackendError     = "backend_error"
)

var validTriggers = map[string]struct{}{
	TriggerTimeout:          {},
	TriggerBackendUnhealthy: {},
	TriggerUpstream4xx:      {},
	TriggerUpstream5xx:      {},
	TriggerBackendError:     {},
}

func IsValidTrigger(trigger string) bool {
	_, ok := validTriggers[trigger]
	return ok
}

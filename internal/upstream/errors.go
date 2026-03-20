package upstream

import "fmt"

type Kind string

const (
	KindTimeout          Kind = "timeout"
	KindBackendUnhealthy Kind = "backend_unhealthy"
	KindUpstream4xx      Kind = "upstream_4xx"
	KindUpstream5xx      Kind = "upstream_5xx"
	KindBackendError     Kind = "backend_error"
)

type Error struct {
	Kind    Kind
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s: %s", e.Kind, e.Message)
}

func New(kind Kind, message string) *Error {
	return &Error{Kind: kind, Message: message}
}

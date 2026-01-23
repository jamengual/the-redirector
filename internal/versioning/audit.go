package versioning

import (
	"encoding/json"
	"sync"
	"time"
)

// AuditEventType represents the type of audit event.
type AuditEventType string

const (
	AuditEventConfigLoaded   AuditEventType = "config_loaded"
	AuditEventConfigReloaded AuditEventType = "config_reloaded"
	AuditEventConfigRollback AuditEventType = "config_rollback"
	AuditEventAuthSuccess    AuditEventType = "auth_success"
	AuditEventAuthFailure    AuditEventType = "auth_failure"
	AuditEventAPICall        AuditEventType = "api_call"
)

// AuditEvent represents a single audit log entry.
type AuditEvent struct {
	Timestamp time.Time      `json:"timestamp"`
	Type      AuditEventType `json:"type"`
	Actor     string         `json:"actor,omitempty"`  // Who performed the action
	Source    string         `json:"source,omitempty"` // IP address or source
	Details   any            `json:"details,omitempty"`
	Version   int            `json:"version,omitempty"` // Config version if applicable
}

// AuditLog maintains a bounded log of audit events.
type AuditLog struct {
	events   []*AuditEvent
	maxSize  int
	current  int
	count    int
	mu       sync.RWMutex
	handlers []AuditHandler
}

// AuditHandler is called when new audit events are recorded.
type AuditHandler func(event *AuditEvent)

// NewAuditLog creates a new audit log with the specified max size.
func NewAuditLog(maxSize int) *AuditLog {
	if maxSize < 1 {
		maxSize = 1000
	}
	return &AuditLog{
		events:  make([]*AuditEvent, maxSize),
		maxSize: maxSize,
	}
}

// AddHandler registers a handler to be called on new events.
func (a *AuditLog) AddHandler(handler AuditHandler) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.handlers = append(a.handlers, handler)
}

// Log records a new audit event.
func (a *AuditLog) Log(eventType AuditEventType, actor, source string, details any) *AuditEvent {
	event := &AuditEvent{
		Timestamp: time.Now(),
		Type:      eventType,
		Actor:     actor,
		Source:    source,
		Details:   details,
	}

	a.mu.Lock()
	a.current = (a.current + 1) % a.maxSize
	a.events[a.current] = event
	if a.count < a.maxSize {
		a.count++
	}
	handlers := a.handlers
	a.mu.Unlock()

	// Call handlers outside lock
	for _, h := range handlers {
		h(event)
	}

	return event
}

// LogConfigChange records a configuration change event.
func (a *AuditLog) LogConfigChange(eventType AuditEventType, version *ConfigVersion, actor, source string) *AuditEvent {
	details := map[string]any{
		"version":     version.Version,
		"hash":        version.Hash,
		"rules_count": version.RulesCount,
		"config_src":  version.Source,
	}

	if version.Changes != nil {
		details["changes"] = version.Changes
	}

	event := &AuditEvent{
		Timestamp: time.Now(),
		Type:      eventType,
		Actor:     actor,
		Source:    source,
		Details:   details,
		Version:   version.Version,
	}

	a.mu.Lock()
	a.current = (a.current + 1) % a.maxSize
	a.events[a.current] = event
	if a.count < a.maxSize {
		a.count++
	}
	handlers := a.handlers
	a.mu.Unlock()

	// Call handlers outside lock
	for _, h := range handlers {
		h(event)
	}

	return event
}

// Recent returns the most recent n events.
func (a *AuditLog) Recent(n int) []*AuditEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	if n > a.count {
		n = a.count
	}

	result := make([]*AuditEvent, 0, n)
	for i := 0; i < n; i++ {
		idx := (a.current - i + a.maxSize) % a.maxSize
		if a.events[idx] != nil {
			result = append(result, a.events[idx])
		}
	}
	return result
}

// Query returns events matching the filter criteria.
func (a *AuditLog) Query(filter AuditFilter) []*AuditEvent {
	a.mu.RLock()
	defer a.mu.RUnlock()

	var result []*AuditEvent
	for i := 0; i < a.count; i++ {
		idx := (a.current - i + a.maxSize) % a.maxSize
		event := a.events[idx]
		if event == nil {
			continue
		}

		// Apply filters
		if filter.Type != "" && event.Type != filter.Type {
			continue
		}
		if filter.Actor != "" && event.Actor != filter.Actor {
			continue
		}
		if !filter.Since.IsZero() && event.Timestamp.Before(filter.Since) {
			continue
		}
		if !filter.Until.IsZero() && event.Timestamp.After(filter.Until) {
			continue
		}

		result = append(result, event)

		if filter.Limit > 0 && len(result) >= filter.Limit {
			break
		}
	}

	return result
}

// AuditFilter specifies criteria for querying audit events.
type AuditFilter struct {
	Type   AuditEventType
	Actor  string
	Since  time.Time
	Until  time.Time
	Limit  int
}

// Count returns the total number of stored events.
func (a *AuditLog) Count() int {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.count
}

// JSON returns the recent events as JSON.
func (a *AuditLog) JSON(n int) ([]byte, error) {
	events := a.Recent(n)
	return json.Marshal(events)
}

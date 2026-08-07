package main

import (
	"time"

	"github.com/dfc-coder/xarlatan/internal/application"
)

// ConsumeFirstSTTPartial forwards metadata-only latency lookup through the
// observer fanout without exposing transcript text on the control plane.
func (o *fanoutObserver) ConsumeFirstSTTPartial(turnID uint64) time.Duration {
	if o == nil {
		return 0
	}
	for _, observer := range o.observers {
		provider, ok := observer.(application.STTPartialMetricProvider)
		if !ok {
			continue
		}
		if latency := provider.ConsumeFirstSTTPartial(turnID); latency > 0 {
			return latency
		}
	}
	return 0
}

var _ application.STTPartialMetricProvider = (*fanoutObserver)(nil)

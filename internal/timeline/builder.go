package timeline

import (
	"sort"

	"github.com/google/uuid"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
)

type Builder struct{}

func New() *Builder {
	return &Builder{}
}

func (b *Builder) Build(data contracts.InvestigationData) []contracts.TimelineEvent {
	var events []contracts.TimelineEvent

	for _, alert := range data.Alerts {
		events = append(events, contracts.TimelineEvent{
			Timestamp:   alert.StartsAt,
			Kind:        contracts.TimelineAlert,
			Source:      alert.Source,
			Narrative:   alert.Title,
			EvidenceIDs: []string{alert.ID},
			Attributes:  alert.Labels,
		})
	}

	for _, e := range data.Evidence {
		var ev *contracts.TimelineEvent

		switch e.Kind {
		case contracts.EvidenceEvent, contracts.EvidenceK8sState:
			ev = &contracts.TimelineEvent{
				Timestamp:   e.ObservedAtFrom,
				Kind:        contracts.TimelineK8sEvent,
				Source:      e.Source,
				Narrative:   e.Summary,
				EvidenceIDs: []string{e.ID},
				Attributes:  e.Attributes,
			}
		case contracts.EvidenceMetric:
			if e.Score < 0.5 {
				continue
			}
			ev = &contracts.TimelineEvent{
				Timestamp:   e.ObservedAtFrom,
				Kind:        contracts.TimelineMetricShift,
				Source:      e.Source,
				Narrative:   e.Summary,
				EvidenceIDs: []string{e.ID},
				Attributes:  e.Attributes,
			}
		case contracts.EvidenceLog:
			if e.Score < 0.5 {
				continue
			}
			ev = &contracts.TimelineEvent{
				Timestamp:   e.ObservedAtFrom,
				Kind:        contracts.TimelineLogPattern,
				Source:      e.Source,
				Narrative:   e.Summary,
				EvidenceIDs: []string{e.ID},
				Attributes:  e.Attributes,
			}
		case contracts.EvidenceDeploy:
			ev = &contracts.TimelineEvent{
				Timestamp:   e.ObservedAtFrom,
				Kind:        contracts.TimelineDeploy,
				Source:      e.Source,
				Narrative:   e.Summary,
				EvidenceIDs: []string{e.ID},
				Attributes:  e.Attributes,
			}
		}

		if ev != nil {
			events = append(events, *ev)
		}
	}

	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.Before(events[j].Timestamp)
	})

	investigationID := data.Investigation.ID
	for i := range events {
		events[i].ID = uuid.New().String()
		events[i].InvestigationID = investigationID
	}

	return events
}

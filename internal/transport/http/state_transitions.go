package http

import (
	"time"

	"github.com/sentoris-ai/sentoris-proxy/internal/domain"
)

func NewStateTransitionRecorder() *StateTransitionRecorder {
	return &StateTransitionRecorder{
		transitions: make([]domain.StateTransition, 0),
	}
}

type StateTransitionRecorder struct {
	transitions []domain.StateTransition
}

func (r *StateTransitionRecorder) RecordTransition(from domain.ExecutionState, to domain.ExecutionState) {
	r.transitions = append(r.transitions, domain.StateTransition{
		From:      from,
		To:        to,
		Timestamp: time.Now().UTC(),
	})
}

func (r *StateTransitionRecorder) GetTransitions() []domain.StateTransition {
	return r.transitions
}

func (r *StateTransitionRecorder) RecordInit() {
	r.RecordTransition("", domain.StateInit)
}

func (r *StateTransitionRecorder) RecordConstraintEval() {
	lastState := domain.StateInit
	if len(r.transitions) > 0 {
		lastState = r.transitions[len(r.transitions)-1].To
	}
	r.RecordTransition(lastState, domain.StateConstraintEval)
}

func (r *StateTransitionRecorder) RecordExecuting() {
	lastState := domain.StateConstraintEval
	if len(r.transitions) > 0 {
		lastState = r.transitions[len(r.transitions)-1].To
	}
	r.RecordTransition(lastState, domain.StateExecuting)
}

func (r *StateTransitionRecorder) RecordValidation() {
	r.RecordTransition(domain.StateExecuting, domain.StateValidation)
}

func (r *StateTransitionRecorder) RecordFinalized() {
	lastState := domain.StateValidation
	if len(r.transitions) > 0 {
		lastState = r.transitions[len(r.transitions)-1].To
	}
	r.RecordTransition(lastState, domain.StateFinalized)
}

func (r *StateTransitionRecorder) RecordFailed() {
	lastState := domain.StateExecuting
	if len(r.transitions) > 0 {
		lastState = r.transitions[len(r.transitions)-1].To
	}
	r.RecordTransition(lastState, domain.StateFailed)
}

func (r *StateTransitionRecorder) ApplyToObservations(observations *domain.Observations) {
	observations.StateTransitions = r.transitions
}

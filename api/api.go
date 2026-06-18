package api

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/rahulkosamkar/sherlock/internal/contracts"
	"go.uber.org/zap"
)

type InvestigationReader interface {
	GetByID(ctx context.Context, id string) (*contracts.Investigation, error)
}

type EvidenceReader interface {
	ListByInvestigation(ctx context.Context, investigationID string) ([]contracts.Evidence, error)
}

type TimelineReader interface {
	ListByInvestigation(ctx context.Context, investigationID string) ([]contracts.TimelineEvent, error)
}

type HypothesisReader interface {
	ListByInvestigation(ctx context.Context, investigationID string) ([]contracts.Hypothesis, error)
}

type Server struct {
	investigations   InvestigationReader
	evidence         EvidenceReader
	timelines        TimelineReader
	hypotheses       HypothesisReader
	apiKeyMiddleware func(http.Handler) http.Handler
	logger           *zap.Logger
}

func NewServer(
	investigations InvestigationReader,
	evidence EvidenceReader,
	timelines TimelineReader,
	hypotheses HypothesisReader,
	logger *zap.Logger,
) *Server {
	return &Server{
		investigations: investigations,
		evidence:       evidence,
		timelines:      timelines,
		hypotheses:     hypotheses,
		logger:         logger,
	}
}

func (s *Server) SetAPIKeyAuth(mw func(http.Handler) http.Handler) {
	s.apiKeyMiddleware = mw
}

func (s *Server) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)

	r.Get("/api/v1/health", s.handleHealth)
	r.Group(func(r chi.Router) {
		if s.apiKeyMiddleware != nil {
			r.Use(s.apiKeyMiddleware)
		}
		r.Get("/api/v1/investigations/{id}", s.handleGetInvestigation)
		r.Get("/api/v1/investigations/{id}/evidence", s.handleListEvidence)
		r.Get("/api/v1/investigations/{id}/timeline", s.handleListTimeline)
		r.Get("/api/v1/investigations/{id}/hypotheses", s.handleListHypotheses)
	})

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{
		"status":  "ok",
		"service": "sherlock",
	})
}

func (s *Server) handleGetInvestigation(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	inv, err := s.investigations.GetByID(r.Context(), id)
	if err != nil {
		s.logger.Error("failed to get investigation", zap.String("id", id), zap.Error(err))
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "investigation not found"})
		return
	}
	writeJSON(w, http.StatusOK, inv)
}

func (s *Server) handleListEvidence(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	evidence, err := s.evidence.ListByInvestigation(r.Context(), id)
	if err != nil {
		s.logger.Error("failed to list evidence", zap.String("investigation_id", id), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list evidence"})
		return
	}
	writeJSON(w, http.StatusOK, evidence)
}

func (s *Server) handleListTimeline(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	events, err := s.timelines.ListByInvestigation(r.Context(), id)
	if err != nil {
		s.logger.Error("failed to list timeline", zap.String("investigation_id", id), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list timeline"})
		return
	}
	writeJSON(w, http.StatusOK, events)
}

func (s *Server) handleListHypotheses(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	hypotheses, err := s.hypotheses.ListByInvestigation(r.Context(), id)
	if err != nil {
		s.logger.Error("failed to list hypotheses", zap.String("investigation_id", id), zap.Error(err))
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "failed to list hypotheses"})
		return
	}
	writeJSON(w, http.StatusOK, hypotheses)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

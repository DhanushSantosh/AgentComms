package authority

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/DhanushSantosh/AgentComms/internal/controlplane"
)

const (
	maxHTTPBodyBytes     = controlplane.MaxCommandBytes + 16*1024
	defaultAdmission     = 256
	defaultRatePerSecond = 100
	defaultRateBurst     = 200
	streamPollInterval   = 250 * time.Millisecond
	streamHeartbeat      = 15 * time.Second
	maxRateBuckets       = 10_000
	rateBucketIdleTTL    = 10 * time.Minute
)

type HTTPConfig struct {
	MaxInFlight   int
	RatePerSecond float64
	RateBurst     float64
	Logger        *slog.Logger
}

type HTTPServer struct {
	engine    *Engine
	logger    *slog.Logger
	admission chan struct{}
	rates     *rateRegistry
	metrics   serverMetrics
}

type serverMetrics struct {
	requests       atomic.Uint64
	rejected       atomic.Uint64
	mutations      atomic.Uint64
	conflicts      atomic.Uint64
	signatureFails atomic.Uint64
	inFlight       atomic.Int64
	mutationNanos  atomic.Uint64
}

func NewHTTPServer(engine *Engine, cfg HTTPConfig) *HTTPServer {
	maxInFlight := cfg.MaxInFlight
	if maxInFlight <= 0 {
		maxInFlight = defaultAdmission
	}
	rate := cfg.RatePerSecond
	if rate <= 0 {
		rate = defaultRatePerSecond
	}
	burst := cfg.RateBurst
	if burst <= 0 {
		burst = defaultRateBurst
	}
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &HTTPServer{
		engine: engine, logger: logger, admission: make(chan struct{}, maxInFlight),
		rates: newRateRegistry(rate, burst),
	}
}

func (s *HTTPServer) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health/live", s.live)
	mux.HandleFunc("GET /health/ready", s.ready)
	mux.HandleFunc("GET /metrics", s.serveMetrics)
	mux.HandleFunc("POST /v1/projects", s.createProject)
	mux.HandleFunc("POST /v1/projects/{project}/commands", s.command)
	mux.HandleFunc("GET /v1/projects/{project}/state", s.state)
	mux.HandleFunc("GET /v1/projects/{project}/events", s.events)
	mux.HandleFunc("GET /v1/projects/{project}/stream", s.stream)
	mux.HandleFunc("POST /v1/projects/{project}/verify", s.verify)
	return s.middleware(mux)
}

func (s *HTTPServer) middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.metrics.requests.Add(1)
		select {
		case s.admission <- struct{}{}:
			s.metrics.inFlight.Add(1)
			defer func() {
				<-s.admission
				s.metrics.inFlight.Add(-1)
			}()
		default:
			s.metrics.rejected.Add(1)
			w.Header().Set("Retry-After", "1")
			writeError(w, http.StatusServiceUnavailable, &controlplane.Error{
				Code: controlplane.CodeUnavailable, Message: "service admission queue is full", RetryAfter: time.Second,
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) live(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"status": "live"})
}

func (s *HTTPServer) ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.engine.Healthy(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, &controlplane.Error{Code: controlplane.CodeUnavailable, Message: "database is unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
}

func (s *HTTPServer) createProject(w http.ResponseWriter, r *http.Request) {
	var request struct {
		ProjectID string `json:"project_id"`
		OwnerID   string `json:"owner_id"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := s.engine.CreateProject(r.Context(), request.ProjectID, request.OwnerID); err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"project_id": request.ProjectID, "owner_id": request.OwnerID})
}

func (s *HTTPServer) command(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	projectID := r.PathValue("project")
	var command controlplane.Command
	if !decodeJSON(w, r, &command) {
		return
	}
	if command.ProjectID != projectID {
		writeError(w, http.StatusBadRequest, &controlplane.Error{Code: controlplane.CodeValidation, Message: "path and command project IDs differ"})
		return
	}
	rateKey := projectID + ":" + command.Actor
	allowed, retryAfter := s.rates.allow(rateKey, time.Now())
	if !allowed {
		s.metrics.rejected.Add(1)
		w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter.Seconds()))))
		writeError(w, http.StatusTooManyRequests, &controlplane.Error{
			Code: controlplane.CodeRateLimited, Message: "actor mutation rate exceeded", RetryAfter: retryAfter,
		})
		return
	}
	event, receipt, err := s.engine.Mutate(r.Context(), command)
	if err != nil {
		var controlErr *controlplane.Error
		if errors.As(err, &controlErr) {
			if controlErr.Code == controlplane.CodeConflict {
				s.metrics.conflicts.Add(1)
			}
			if controlErr.Code == controlplane.CodeIntegrity {
				s.metrics.signatureFails.Add(1)
			}
		}
		writeControlError(w, err)
		return
	}
	s.metrics.mutations.Add(1)
	s.metrics.mutationNanos.Add(uint64(time.Since(started)))
	writeJSON(w, http.StatusOK, map[string]any{
		"event": event, "metadata": controlplane.ResultMetadata{
			Consistency: "AUTHORITATIVE", ServerSequence: event.Sequence, Receipt: &receipt, Connectivity: "ONLINE",
		},
	})
}

func (s *HTTPServer) state(w http.ResponseWriter, r *http.Request) {
	state, metadata, err := s.engine.State(r.Context(), r.PathValue("project"))
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": state, "metadata": metadata})
}

func (s *HTTPServer) events(w http.ResponseWriter, r *http.Request) {
	page, err := pageRequest(r)
	if err != nil {
		writeControlError(w, err)
		return
	}
	result, err := s.engine.Events(r.Context(), r.PathValue("project"), page)
	if err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *HTTPServer) stream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeError(w, http.StatusInternalServerError, &controlplane.Error{Code: controlplane.CodeUnavailable, Message: "streaming is unsupported"})
		return
	}
	cursor := r.URL.Query().Get("cursor")
	if _, err := controlplane.DecodeCursor(cursor); err != nil {
		writeControlError(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	poll := time.NewTicker(streamPollInterval)
	heartbeat := time.NewTicker(streamHeartbeat)
	defer poll.Stop()
	defer heartbeat.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-heartbeat.C:
			_, _ = fmt.Fprint(w, ": heartbeat\n\n")
			flusher.Flush()
		case <-poll.C:
			page, err := s.engine.Events(r.Context(), r.PathValue("project"), controlplane.PageRequest{Cursor: cursor, Limit: controlplane.DefaultPageSize})
			if err != nil {
				s.logger.Error("stream query failed", "project", r.PathValue("project"), "error", err)
				return
			}
			for _, record := range page.Items {
				raw, _ := json.Marshal(record)
				_, _ = fmt.Fprintf(w, "id: %s\nevent: project-event\ndata: %s\n\n", controlplane.EncodeCursor(record.Event.Sequence), raw)
				cursor = controlplane.EncodeCursor(record.Event.Sequence)
			}
			if len(page.Items) > 0 {
				flusher.Flush()
			}
		}
	}
}

func (s *HTTPServer) verify(w http.ResponseWriter, r *http.Request) {
	var request struct {
		From uint64 `json:"from,omitempty"`
		To   uint64 `json:"to,omitempty"`
	}
	if !decodeJSON(w, r, &request) {
		return
	}
	if err := s.engine.VerifyRange(r.Context(), r.PathValue("project"), request.From, request.To); err != nil {
		writeControlError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"verified": true, "from": request.From, "to": request.To})
}

func (s *HTTPServer) serveMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	stats := s.engine.db.Stats()
	outbox, _ := s.engine.OutboxStats(r.Context())
	_, _ = fmt.Fprintf(w, `# TYPE agent_comms_http_requests_total counter
agent_comms_http_requests_total %d
# TYPE agent_comms_http_rejected_total counter
agent_comms_http_rejected_total %d
# TYPE agent_comms_mutations_total counter
agent_comms_mutations_total %d
# TYPE agent_comms_conflicts_total counter
agent_comms_conflicts_total %d
# TYPE agent_comms_signature_failures_total counter
agent_comms_signature_failures_total %d
# TYPE agent_comms_http_in_flight gauge
agent_comms_http_in_flight %d
# TYPE agent_comms_mutation_duration_seconds_sum counter
agent_comms_mutation_duration_seconds_sum %f
# TYPE agent_comms_outbox_pending gauge
agent_comms_outbox_pending %d
# TYPE agent_comms_database_open_connections gauge
agent_comms_database_open_connections %d
# TYPE agent_comms_database_wait_count counter
agent_comms_database_wait_count %d
`, s.metrics.requests.Load(), s.metrics.rejected.Load(), s.metrics.mutations.Load(),
		s.metrics.conflicts.Load(), s.metrics.signatureFails.Load(), s.metrics.inFlight.Load(),
		float64(s.metrics.mutationNanos.Load())/float64(time.Second), outbox.Pending,
		stats.OpenConnections, stats.WaitCount)
}

func pageRequest(r *http.Request) (controlplane.PageRequest, error) {
	page := controlplane.PageRequest{Cursor: r.URL.Query().Get("cursor")}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		limit, err := strconv.Atoi(raw)
		if err != nil {
			return page, &controlplane.Error{Code: controlplane.CodeValidation, Message: "limit must be an integer"}
		}
		page.Limit = limit
	}
	_, err := page.BoundedLimit()
	return page, err
}

func decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	return decodeJSONLimit(w, r, target, maxHTTPBodyBytes)
}

func decodeJSONLimit(w http.ResponseWriter, r *http.Request, target any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeError(w, http.StatusBadRequest, &controlplane.Error{Code: controlplane.CodeValidation, Message: "invalid request: " + err.Error()})
		return false
	}
	return true
}

func writeControlError(w http.ResponseWriter, err error) {
	var controlErr *controlplane.Error
	if !errors.As(err, &controlErr) {
		controlErr = &controlplane.Error{Code: controlplane.CodeUnavailable, Message: err.Error()}
	}
	status := http.StatusBadRequest
	switch controlErr.Code {
	case controlplane.CodeAuthorization:
		status = http.StatusForbidden
	case controlplane.CodeIntegrity:
		status = http.StatusUnprocessableEntity
	case controlplane.CodeConflict, controlplane.CodeStalePrecondition:
		status = http.StatusConflict
	case controlplane.CodeRateLimited:
		status = http.StatusTooManyRequests
	case controlplane.CodeOffline, controlplane.CodeUnavailable:
		status = http.StatusServiceUnavailable
	}
	writeError(w, status, controlErr)
}

func writeError(w http.ResponseWriter, status int, err *controlplane.Error) {
	writeJSON(w, status, map[string]any{"error": map[string]any{
		"code": err.Code, "message": err.Message, "retry_after_ms": err.RetryAfter.Milliseconds(),
	}})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

type rateRegistry struct {
	mu      sync.Mutex
	rate    float64
	burst   float64
	buckets map[string]rateBucket
}

type rateBucket struct {
	tokens float64
	at     time.Time
}

func newRateRegistry(rate, burst float64) *rateRegistry {
	return &rateRegistry{rate: rate, burst: burst, buckets: map[string]rateBucket{}}
}

func (r *rateRegistry) allow(key string, now time.Time) (bool, time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	bucket, ok := r.buckets[key]
	if !ok && len(r.buckets) >= maxRateBuckets {
		cutoff := now.Add(-rateBucketIdleTTL)
		for existingKey, existingBucket := range r.buckets {
			if existingBucket.at.Before(cutoff) {
				delete(r.buckets, existingKey)
			}
		}
		if len(r.buckets) >= maxRateBuckets {
			return false, time.Second
		}
	}
	if !ok {
		bucket = rateBucket{tokens: r.burst, at: now}
	}
	elapsed := now.Sub(bucket.at).Seconds()
	bucket.tokens = min(r.burst, bucket.tokens+elapsed*r.rate)
	bucket.at = now
	if bucket.tokens >= 1 {
		bucket.tokens--
		r.buckets[key] = bucket
		return true, 0
	}
	r.buckets[key] = bucket
	wait := time.Duration((1 - bucket.tokens) / r.rate * float64(time.Second))
	return false, wait
}

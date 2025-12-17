package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Ryley4/NYCTcord/backend/internal/db"
	"github.com/go-chi/chi/v5"
)

type Server struct {
	DB *db.DB
}

func NewServer(database *db.DB) *Server {
	return &Server{DB: database}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Get("/health", s.handleHealth)
	r.Get("/api/lines", s.handleGetLines)

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

type LineStatus struct {
	LineID    string    `json:"line_id"`
	Status    string    `json:"status"`
	Header    *string   `json:"header,omitempty"`
	Body      *string   `json:"body,omitempty"`
	Effect    *string   `json:"effect,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s *Server) handleGetLines(w http.ResponseWriter, r *http.Request) {
	rows, err := s.DB.Query(`
        SELECT line_id, status, header, body, effect, updated_at 
        FROM line_status
        ORDER BY line_id
    `)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	var lines []LineStatus

	for rows.Next() {
		var ls LineStatus
		var header, body, effect *string
		var updated string

		if err := rows.Scan(
			&ls.LineID,
			&ls.Status,
			&header,
			&body,
			&effect,
			&updated,
		); err != nil {
			http.Error(w, "scan error", http.StatusInternalServerError)
			return
		}

		ls.Header = header
		ls.Body = body
		ls.Effect = effect

		t, err := time.Parse(time.RFC3339Nano, updated)
		if err != nil {
			t, _ = time.Parse("2006-01-02 15:04:05", updated)
		}
		ls.UpdatedAt = t

		lines = append(lines, ls)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(lines)
}

package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/Ryley4/NYCTcord/backend/internal/db"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"
)

type Server struct {
	DB *db.DB
}

func NewServer(database *db.DB) *Server {
	return &Server{DB: database}
}

type LineStatus struct {
	LineID    string    `json:"line_id"`
	Status    string    `json:"status"`
	Header    *string   `json:"header,omitempty"`
	Body      *string   `json:"body,omitempty"`
	Effect    *string   `json:"effect,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Subscription struct {
	ID       int64     `json:"id"`
	LineID   string    `json:"line_id"`
	ViaDM    bool      `json:"via_dm"`
	ViaGuild bool      `json:"via_guild"`
	Created  time.Time `json:"created_at"`
}

type setSubscriptionsRequest struct {
	Lines    []string `json:"lines"`
	ViaDM    bool     `json:"via_dm"`
	ViaGuild bool     `json:"via_guild"`
}

func (s *Server) currentUserID(r *http.Request) int64 {
	return 1
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()

	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:3000"},
		AllowedMethods:   []string{"GET", "POST", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	r.Get("/health", s.handleHealth)

	r.Route("/api", func(r chi.Router) {
		r.Get("/lines", s.handleGetLines)
		r.Get("/subscriptions", s.handleGetSubscriptions)
		r.Post("/subscriptions", s.handleSetSubscriptions)
	})

	return r
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
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

	lines := make([]LineStatus, 0)

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

func (s *Server) handleGetSubscriptions(w http.ResponseWriter, r *http.Request) {
	userID := s.currentUserID(r)

	rows, err := s.DB.Query(`
        SELECT id, line_id, via_dm, via_guild, created_at
        FROM subscriptions
        WHERE user_id = ?
        ORDER BY line_id
    `, userID)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	subs := make([]Subscription, 0)

	for rows.Next() {
		var sub Subscription
		var viaDMInt, viaGuildInt int
		var created string

		if err := rows.Scan(
			&sub.ID,
			&sub.LineID,
			&viaDMInt,
			&viaGuildInt,
			&created,
		); err != nil {
			http.Error(w, "scan error", http.StatusInternalServerError)
			return
		}

		sub.ViaDM = viaDMInt == 1
		sub.ViaGuild = viaGuildInt == 1

		t, err := time.Parse(time.RFC3339Nano, created)
		if err != nil {
			t, _ = time.Parse("2006-01-02 15:04:05", created)
		}
		sub.Created = t

		subs = append(subs, sub)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(subs)
}

func (s *Server) handleSetSubscriptions(w http.ResponseWriter, r *http.Request) {
	userID := s.currentUserID(r)

	var req setSubscriptionsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}

	tx, err := s.DB.Begin()
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM subscriptions WHERE user_id = ?`, userID); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	viaDMInt := 0
	if req.ViaDM {
		viaDMInt = 1
	}
	viaGuildInt := 0
	if req.ViaGuild {
		viaGuildInt = 1
	}

	stmt, err := tx.Prepare(`
        INSERT INTO subscriptions (user_id, line_id, via_dm, via_guild)
        VALUES (?, ?, ?, ?)
    `)
	if err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}
	defer stmt.Close()

	for _, line := range req.Lines {
		if _, err := stmt.Exec(userID, line, viaDMInt, viaGuildInt); err != nil {
			http.Error(w, "db error", http.StatusInternalServerError)
			return
		}
	}

	if err := tx.Commit(); err != nil {
		http.Error(w, "db error", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

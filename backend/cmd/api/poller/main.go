package main

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	gtfsrt "github.com/MobilityData/gtfs-realtime-bindings/golang/gtfs"
	"github.com/Ryley4/NYCTcord/backend/internal/db"
	"google.golang.org/protobuf/proto"
)

var defaultFeeds = []string{
	"https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/camsys%2Fsubway-alerts",
}

type bestAlert struct {
	effect string
	header string
	body   string
	hash   string
}

func main() {
	database, err := db.Open("nyctcord.db")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	feeds := defaultFeeds
	if v := strings.TrimSpace(os.Getenv("MTA_FEEDS")); v != "" {
		parts := strings.Split(v, ",")
		tmp := make([]string, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				tmp = append(tmp, p)
			}
		}
		if len(tmp) > 0 {
			feeds = tmp
		}
	}

	client := &http.Client{Timeout: 15 * time.Second}

	runOnce(database, client, feeds)

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		runOnce(database, client, feeds)
	}
}

func runOnce(database *db.DB, client *http.Client, feeds []string) {
	log.Printf("poller: fetching %d feeds", len(feeds))

	bestByLine := map[string]bestAlert{}
	now := uint64(time.Now().Unix())

	for _, url := range feeds {
		msg, err := fetchFeed(client, url)
		if err != nil {
			log.Printf("poller: fetch error (%s): %v", url, err)
			continue
		}

		for _, ent := range msg.GetEntity() {
			alert := ent.GetAlert()
			if alert == nil {
				continue
			}
			if !isActiveNow(alert, now) {
				continue
			}

			effect := alert.GetEffect().String()
			header := firstTranslation(alert.GetHeaderText())
			body := firstTranslation(alert.GetDescriptionText())
			h := contentHash(effect, header, body)

			cand := bestAlert{
				effect: effect,
				header: header,
				body:   body,
				hash:   h,
			}

			for _, ie := range alert.GetInformedEntity() {
				lineID := strings.TrimSpace(ie.GetRouteId())
				if lineID == "" {
					continue
				}

				cur, ok := bestByLine[lineID]
				if !ok || severityRank(cand.effect) > severityRank(cur.effect) {
					bestByLine[lineID] = cand
				}
			}
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	changedCount := 0
	for lineID, a := range bestByLine {
		status := statusFromEffect(a.effect)

		changed, err := upsertLineStatusIfChanged(
			ctx,
			database,
			lineID,
			status,
			a.header,
			a.body,
			a.effect,
			a.hash,
		)
		if err != nil {
			log.Printf("poller: upsert error for line %s: %v", lineID, err)
			continue
		}
		if changed {
			changedCount++
		}
	}

	log.Printf("poller: changed %d lines", changedCount)
}

func fetchFeed(client *http.Client, url string) (*gtfsrt.FeedMessage, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		snippet := string(b)
		if len(snippet) > 200 {
			snippet = snippet[:200]
		}
		return nil, fmt.Errorf("HTTP %d from %s: %q", resp.StatusCode, url, snippet)
	}

	var msg gtfsrt.FeedMessage
	if err := proto.Unmarshal(b, &msg); err != nil {
		return nil, fmt.Errorf("unmarshal failed (content-type=%q bytes=%d): %w",
			resp.Header.Get("Content-Type"), len(b), err)
	}
	return &msg, nil
}

func isActiveNow(a *gtfsrt.Alert, now uint64) bool {
	aps := a.GetActivePeriod()
	if len(aps) == 0 {
		return true
	}
	for _, ap := range aps {
		start := ap.GetStart()
		end := ap.GetEnd()
		if (start == 0 || now >= start) && (end == 0 || now <= end) {
			return true
		}
	}
	return false
}

func firstTranslation(t *gtfsrt.TranslatedString) string {
	if t == nil || len(t.GetTranslation()) == 0 {
		return ""
	}
	return t.GetTranslation()[0].GetText()
}

func contentHash(effect, header, body string) string {
	sum := sha256.Sum256([]byte(effect + "\n" + header + "\n" + body))
	return hex.EncodeToString(sum[:])
}

func severityRank(effect string) int {
	switch effect {
	case "NO_SERVICE":
		return 5
	case "REDUCED_SERVICE":
		return 4
	case "SIGNIFICANT_DELAYS":
		return 3
	case "DETOUR":
		return 2
	case "MODIFIED_SERVICE":
		return 1
	default:
		return 0
	}
}

func statusFromEffect(effect string) string {
	switch effect {
	case "NO_SERVICE":
		return "No Service"
	case "REDUCED_SERVICE":
		return "Reduced Service"
	case "SIGNIFICANT_DELAYS":
		return "Delays"
	case "DETOUR", "MODIFIED_SERVICE":
		return "Service Change"
	default:
		return "Alert"
	}
}

func nullIfEmpty(s string) any {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func upsertLineStatusIfChanged(
	ctx context.Context,
	database *db.DB,
	lineID,
	status,
	header,
	body,
	effect,
	hash string,
) (bool, error) {
	var existingHash sql.NullString
	var existingStatus sql.NullString

	err := database.QueryRowContext(ctx,
		`SELECT content_hash, status FROM line_status WHERE line_id = ?`,
		lineID,
	).Scan(&existingHash, &existingStatus)

	if err != nil && err != sql.ErrNoRows {
		return false, err
	}

	if err != sql.ErrNoRows && existingHash.Valid && existingHash.String == hash {
		return false, nil
	}

	oldStatus := ""
	if existingStatus.Valid {
		oldStatus = existingStatus.String
	}

	res, err := database.ExecContext(ctx, `
		INSERT INTO alerts (alert_id, line_id, old_status, new_status, header, body, effect, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, datetime('now'))
	`, "", lineID, oldStatus, status, nullIfEmpty(header), nullIfEmpty(body), nullIfEmpty(effect))
	if err != nil {
		return false, err
	}

	alertRowID, _ := res.LastInsertId()

	_, err = database.ExecContext(ctx, `
		INSERT INTO notifications (user_id, alert_id, line_id, channel_type, status, created_at)
		SELECT s.user_id, ?, ?, 'dm', 'pending', datetime('now')
		FROM subscriptions s
		WHERE s.line_id = ? OR s.line_id = 'ALL'
	`, alertRowID, lineID, lineID)
	if err != nil {
		return false, err
	}

	_, err = database.ExecContext(ctx, `
		INSERT INTO line_status (line_id, status, header, body, effect, content_hash, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(line_id) DO UPDATE SET
			status       = excluded.status,
			header       = excluded.header,
			body         = excluded.body,
			effect       = excluded.effect,
			content_hash = excluded.content_hash,
			updated_at   = excluded.updated_at
	`, lineID, status, nullIfEmpty(header), nullIfEmpty(body), nullIfEmpty(effect), hash)
	if err != nil {
		return false, err
	}

	return true, nil
}

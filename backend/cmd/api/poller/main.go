package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
		feeds = strings.Split(v, ",")
	}

	client := &http.Client{Timeout: 15 * time.Second}

	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	runOnce(database, client, feeds)

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

		for _, ent := range msg.Entity {
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

			for _, ie := range alert.GetInformedEntity() {
				lineID := strings.TrimSpace(ie.GetRouteId())
				if lineID == "" {
					continue
				}

				h := contentHash(effect, header, body)

				cand := bestAlert{
					effect: effect,
					header: header,
					body:   body,
					hash:   h,
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

	for lineID, alert := range bestByLine {
		status := statusFromEffect(alert.effect)
		upsertLineStatus(ctx, database, lineID, status, alert.header, alert.body, alert.effect, alert.hash)
	}

	log.Printf("poller: updated %d lines", len(bestByLine))
}

func fetchFeed(client *http.Client, url string) (*gtfsrt.FeedMessage, error) {
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var msg gtfsrt.FeedMessage
	if err := proto.Unmarshal(b, &msg); err != nil {
		return nil, err
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
	if t == nil || len(t.Translation) == 0 {
		return ""
	}
	return t.Translation[0].GetText()
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

func upsertLineStatus(ctx context.Context, database *db.DB, lineID, status, header, body, effect, hash string) {
	_, _ = database.ExecContext(ctx, `
		INSERT INTO line_status (line_id, status, header, body, effect, updated_at)
		VALUES (?, ?, ?, ?, ?, datetime('now'))
		ON CONFLICT(line_id) DO UPDATE SET
			status=excluded.status,
			header=excluded.header,
			body=excluded.body,
			effect=excluded.effect,
			updated_at=excluded.updated_at
	`, lineID, status, header, body, effect)
}

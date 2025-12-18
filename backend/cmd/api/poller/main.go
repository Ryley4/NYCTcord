package main

import (
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Ryley4/NYCTcord/backend/internal/db"
)

var defaultFeeds = []string{
	"https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs",      // 1-6
	"https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-7",    // 7
	"https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-ace",  // ACE
	"https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-bdfm", // BDFM
	"https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-g",    // G
	"https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-jz",   // JZ
	"https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-nqrw", // NQRW
	"https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-l",    // L
	"https://api-endpoint.mta.info/Dataservice/mtagtfsfeeds/nyct%2Fgtfs-si",   // SIR
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
		feeds = string.Split(v, ",")
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

	now := time.Now().Unix()

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

			jf !isActiveNow(alert, now) {
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
					body: 	body,
					hash: 	h,
				}

				cur, ok := bestByLine[lineID]
				if !ok || severityRank(cand.effect) > severityRank(cur.effect) {
					bestByLine[lineID] = cand
				}
			}
		}
	}

	ctx, cancel : := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for lineID, alert := range bestByLine {
		status := statusFromEffect(alert.effect)
		upsertLineStatus(ctx, database, lineID, status, alert.header, alert., alert.effect, alert.hash)
	}

	log.Printf("poller: updated %d lines", len(bestByLine))
}

func fetchFeed(client *http.Client, url string) (*gtfsrt.FeedMessage, error) {
}
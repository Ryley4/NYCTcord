package main

import (
	"log"
	"net/http"
	"time"

	"github.com/Ryley4/NYCTcord/backend/internal/api"
	"github.com/Ryley4/NYCTcord/backend/internal/db"
)

func main() {
	database, err := db.Open("nyctcord.db")
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Close()

	server := api.NewServer(database)
	router := server.Router()

	srv := &http.Server{
		Addr:         ":8080",
		Handler:      router,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
	}

	log.Println("nyctcord API listening on :8080")
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server error: %v", err)
	}
}

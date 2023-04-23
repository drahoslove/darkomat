package main

import (
	"encoding/json"
	"html/template"
	"log"
	"net/http"
	"os"
	"time"
)

const CSV_URL = "https://www.alik.cz/s/darky/csv"
const GOB_FILE = "data.gob"
const JSON_FILE = "gifts.json"
const INTERVAL = 10 * time.Minute

var PORT = "4576" // tcp port for the http server

func init() {
	if port := os.Getenv("PORT"); port != "" {
		PORT = port
	}
}

var gifts = &Gifts{}

func main() {
	log.Println("loading gob file")
	gifts.load()
	log.Println("loaded")

	if len(*gifts) == 0 {
		log.Println("nothing loaded, trying json")
		gifts.loadJson()
		gifts.refresh(roundTime(time.Now()))
		log.Println("initial refresh")
		err := gifts.save()
		if err != nil {
			log.Println(err)
		}
	}

	go func() { // concurent refreshing loop
		log.Println("starting refresh refreshing")
		time.Sleep(toNextRoundTime())
		for t := range time.Tick(INTERVAL) {
			log.Println(t)
			gifts.refresh(roundTime(t))
		}
	}()

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		setupResponse(&w, r)
		templ := template.Must(template.New("index.html").Funcs(template.FuncMap{
			"formatTime": FormatTime,
		}).ParseFiles("index.html"))

		err := templ.Execute(w, gifts)
		if err != nil {
			log.Println(err)
		}
	})
	http.HandleFunc("/gifts.json", func(w http.ResponseWriter, r *http.Request) {
		setupResponse(&w, r)
		err := json.NewEncoder(w).Encode(gifts)
		if err != nil {
			log.Println(err)
		}
	})
	log.Println("listening on port", PORT)

	log.Println("total:", len(*gifts))
	log.Println("added:", len(gifts.FilterAdded("8760h")))
	log.Println("discounted:", len(gifts.FilterDiscounted("8760h")))
	log.Println("stockchanged:", len(gifts.FilterStockChanged("8760h")))

	if err := http.ListenAndServe(":"+PORT, nil); err != nil {
		log.Fatal(err)
	}
}

func roundTime(t time.Time) time.Time {
	return t.Truncate(INTERVAL)
}

func toNextRoundTime() time.Duration {
	return INTERVAL - time.Since(roundTime(time.Now()))
}

func setupResponse(w *http.ResponseWriter, req *http.Request) {
	(*w).Header().Set("Access-Control-Allow-Origin", "*")
	(*w).Header().Set("Access-Control-Allow-Methods", "GET")
	(*w).Header().Set("Access-Control-Allow-Headers", "Accept, Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization")
}

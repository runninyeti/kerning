package main

import (
	"flag"
	"log"
	"net/http"
)

const defaultIndex = "https://www.centene.com/content/dam/centene/Centene%20Corporate/json/DOCUMENT/2026-09-28_fidelis_index.json"

func main() {
	index := flag.String("index", defaultIndex, "Transparency in Coverage index file URL")
	addr := flag.String("addr", ":8080", "listen address")
	flag.Parse()

	s, err := load(*index)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("loaded %s: %d procedures, %+v", s.Source.File, len(s.Procedures), s.Counts)

	http.HandleFunc("GET /api/rates", s.rates)
	http.HandleFunc("GET /api/procedures", s.procedures)
	http.Handle("GET /", http.FileServer(http.Dir("web/dist")))

	log.Printf("listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, nil))
}

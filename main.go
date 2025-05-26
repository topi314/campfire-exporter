package main

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"encoding/json"
	"flag"
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"slices"
	"time"
)

const graphQLEndpoint = "https://niantic-social-api.nianticlabs.com/graphql"

//go:embed query.graphql
var graphQLQuery string

func main() {
	url := flag.String("url", "", "The URL to the campfire evebt e.g. https://campfire.nianticlabs.com/discover/meetup/7d5719a2-e1a2-4d04-9638-e60eb35728bf")
	output := flag.String("o", "export.csv", "Output file name (default: export.csv)")
	debug := flag.Bool("debug", false, "Enable debug logging")
	flag.Parse()

	if *url == "" {
		flag.Usage()
		return
	}

	eventID := path.Base(*url)
	if eventID == "" {
		log.Fatalf("Invalid URL: %s", *url)
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	buf := &bytes.Buffer{}
	if err := json.NewEncoder(buf).Encode(Req{
		Query: graphQLQuery,
		Variables: map[string]any{
			"id":         eventID,
			"isLoggedIn": false,
			"pageSize":   10000000000, // Large enough to fetch all members
		},
	}); err != nil {
		log.Fatalf("Failed to encode request body: %s", err)
	}

	rq, err := http.NewRequest(http.MethodPost, graphQLEndpoint, buf)
	if err != nil {
		log.Fatalf("Failed to create request: %s", err)
	}

	rq.Header.Set("Content-Type", "application/json")
	rq.Header.Set("Accept", "application/json")

	rs, err := client.Do(rq)
	if err != nil {
		log.Fatalf("Failed to send request: %s", err)
	}
	defer rs.Body.Close()

	if rs.StatusCode != http.StatusOK {
		data, _ := io.ReadAll(rs.Body)
		log.Fatalf("Request failed with status code: %d, response: %s", rs.StatusCode, data)
	}

	var resp Resp

	logBuf := &bytes.Buffer{}
	bodyReader := io.TeeReader(rs.Body, logBuf)

	if err = json.NewDecoder(bodyReader).Decode(&resp); err != nil {
		log.Fatalf("Failed to decode response: %s, response: %s", err, logBuf.String())
	}
	if *debug {
		log.Printf("Response: %+v", resp)
	}

	file, err := os.OpenFile(*output, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		log.Fatalf("Failed to open output file %q: %s", *output, err)
	}
	defer file.Close()

	records := [][]string{
		{"id", "name", "status"},
	}
	for _, rsvpStatus := range resp.Data.Event.RSVPStatuses {
		i := slices.IndexFunc(resp.Data.Event.Members.Edges, func(e MemberEdge) bool {
			return e.Node.ID == rsvpStatus.UserID
		})
		if i < 0 {
			log.Printf("Warning: RSVP member %s not found", rsvpStatus.UserID)
			continue
		}
		records = append(records, []string{
			rsvpStatus.UserID,
			resp.Data.Event.Members.Edges[i].Node.DisplayName,
			rsvpStatus.RSVPStatus,
		})
	}

	if err = csv.NewWriter(file).WriteAll(records); err != nil {
		log.Fatalf("Failed to write CSV: %s", err)
	}

	log.Printf("Wrote %d members to %s", len(resp.Data.Event.Members.Edges), *output)
}

// p0probe explicitly exercises anonymous catalog calls and prints metadata only.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/voici5986/otoneigo/internal/catalog"
	"os"
)

func main() {
	query := flag.String("query", "周杰伦", "anonymous search query")
	id := flag.String("song", "", "song ID; defaults to first search result")
	quality := flag.String("quality", "standard", "requested direct quality")
	flag.Parse()
	if err := run(*query, *id, *quality); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(query, id, quality string) error {
	c := catalog.NewClient()
	ctx := context.Background()
	r, e := c.Search(ctx, query, 0)
	if e != nil {
		return e
	}
	fmt.Printf("search: tracks=%d total=%d\n", len(r.Tracks), r.Total)
	if id == "" {
		if len(r.Tracks) == 0 {
			return nil
		}
		id = r.Tracks[0].NeteaseSongID
	}
	l, e := c.Lyrics(ctx, id)
	if e != nil {
		return e
	}
	fmt.Printf("lyrics: originalBytes=%d translatedBytes=%d\n", len(l.Original), len(l.Translated))
	stream, e := c.Resolve(ctx, id, quality)
	if e != nil {
		return e
	}
	// Never serialize the ResolvedStream: its URL can contain access-bearing data.
	return json.NewEncoder(os.Stdout).Encode(struct {
		SongID   string `json:"songId"`
		Status   string `json:"status"`
		Quality  string `json:"actualQuality"`
		Format   string `json:"actualFormat"`
		Duration int64  `json:"durationMs"`
		HasURL   bool   `json:"hasUrl"`
	}{id, stream.Status, stream.ActualQuality, stream.ActualFormat, stream.DurationMs, stream.URL != ""})
}

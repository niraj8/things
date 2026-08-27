// Domain expiry notifier. Reads domains from a txt file (one per line, # comments),
// asks RDAP for the expiration date, prints anything expiring soon.
//
//	go run ./domains/expiry -f domains.txt -days 30
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

type rdapResp struct {
	Events []struct {
		Action string `json:"eventAction"`
		Date   string `json:"eventDate"`
	} `json:"events"`
}

type result struct {
	domain string
	expiry time.Time
	days   int
	err    error
}

func expiry(domain string) (time.Time, error) {
	resp, err := http.Get("https://rdap.org/domain/" + domain)
	if err != nil {
		return time.Time{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return time.Time{}, fmt.Errorf("rdap %s", resp.Status)
	}
	var r rdapResp
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return time.Time{}, err
	}
	for _, e := range r.Events {
		if strings.Contains(strings.ToLower(e.Action), "expiration") {
			return time.Parse(time.RFC3339, e.Date)
		}
	}
	return time.Time{}, fmt.Errorf("no expiration event")
}

func main() {
	file := flag.String("f", "domains.txt", "file with one domain per line")
	days := flag.Int("days", 30, "warn when expiry is within this many days")
	all := flag.Bool("all", false, "print every domain, not just the ones expiring soon")
	flag.Parse()

	raw, err := os.ReadFile(*file)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	ch := make(chan result)
	n := 0
	for _, line := range strings.Split(string(raw), "\n") {
		d := strings.TrimSpace(line)
		if d == "" || strings.HasPrefix(d, "#") {
			continue
		}
		n++
		go func(d string) {
			t, err := expiry(d)
			ch <- result{domain: d, expiry: t, days: int(time.Until(t).Hours() / 24), err: err}
		}(d)
	}

	var results []result
	for i := 0; i < n; i++ {
		results = append(results, <-ch)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].days < results[j].days })

	fail := false
	for _, r := range results {
		switch {
		case r.err != nil:
			fmt.Printf("%-30s ERROR %v\n", r.domain, r.err)
			fail = true
		case r.days <= *days:
			fmt.Printf("%-30s %s (%d days)\n", r.domain, r.expiry.Format("2006-01-02"), r.days)
		case *all:
			fmt.Printf("%-30s %s (%d days)\n", r.domain, r.expiry.Format("2006-01-02"), r.days)
		}
	}

	if fail {
		os.Exit(1)
	}
}

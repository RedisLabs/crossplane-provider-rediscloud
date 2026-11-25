// A fake-only proxy for the hosted boundary experiment. It contains no Cloud
// upstream and accepts only the fixed synthetic credentials and region route.
package main

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

func main() {
	address := os.Getenv("LISTEN_ADDRESS")
	if address != ":5556" {
		panic("unexpected fixture listener")
	}
	server := &http.Server{Addr: address, ReadHeaderTimeout: 5 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.RequestURI != "/regions" || r.ContentLength != 0 || len(r.TransferEncoding) != 0 || r.Header.Get("X-Api-Key") != "fixture-key" || r.Header.Get("X-Api-Secret-Key") != "fixture-secret" {
			http.Error(w, "denied", http.StatusForbidden)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"regions":[{"id":22,"name":"us-central1","provider":"GCP"}]}`)
	})}
	if err := server.ListenAndServe(); err != nil {
		panic(err)
	}
}

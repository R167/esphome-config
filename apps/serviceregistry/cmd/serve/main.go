package main

import (
	"net/http"
	"time"

	"github.com/R167/esphome-config/apps/serviceregistry"
)

func main() {
	registry := serviceregistry.NewConfigRegistry(10 * time.Minute)
	http.ListenAndServe(":8080", registry.Mux())
}

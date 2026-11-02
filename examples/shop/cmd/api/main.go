// Command api is a made-up order service. It serves HTTP with ServeMux and
// calls a payment provider over HTTPS, so net/http, crypto/tls and
// crypto/x509 defaults apply to it.
package main

import (
	"log"
	"net/http"

	"example.com/shop/internal/orders"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/orders/", orders.Handler)
	log.Fatal(http.ListenAndServe(":8080", mux))
}

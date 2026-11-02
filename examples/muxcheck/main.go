// Command muxcheck prints how ServeMux treats a Go 1.22 style pattern.
// Under httpmuxgo121=1, "GET /orders/{id}" is a literal path, so a request
// to /orders/42 gets a 404. Under the Go 1.22 default it matches.
package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "matched")
	})
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest("GET", "/orders/42", nil))
	fmt.Println("GET /orders/42:", rec.Code)
}

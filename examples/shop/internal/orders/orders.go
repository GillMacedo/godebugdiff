// Package orders handles order requests for the made-up shop.
package orders

import (
	"fmt"
	"net/http"
)

// Handler returns the order ID from the path.
func Handler(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "order %s\n", r.URL.Path[len("/orders/"):])
}

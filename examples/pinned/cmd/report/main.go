// Command report is a made-up tool whose go.mod pins every GODEBUG default
// to Go 1.21 with "godebug default=go1.21", so raising the go line
// changes no defaults.
package main

import (
	"fmt"
	"net/http"
)

func main() {
	fmt.Println(http.StatusText(http.StatusOK))
}

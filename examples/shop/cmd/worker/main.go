// Command worker is a made-up batch job. It only imports fmt and
// math/rand, so most GODEBUG changes don't reach it.
package main

import (
	"fmt"
	"math/rand"
)

func main() {
	rand.Seed(42)
	fmt.Println("picked batch", rand.Intn(10))
}

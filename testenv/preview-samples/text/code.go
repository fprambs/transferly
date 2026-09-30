package main

import (
	"fmt"
	"net/http"
)

// hello answers every request with a greeting.
func hello(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Hello, %s!\n", r.URL.Query().Get("name"))
}

func main() {
	http.HandleFunc("/", hello)
	_ = http.ListenAndServe(":8080", nil)
}

package main

import (
	"log"
	"net/http"
)

func main() {

	log.Println("Concurrency test server started...")

	go StartTest("1000", 1)

	err := http.ListenAndServe(":3333", nil)
	if err != nil {
		panic(err)
	}

}

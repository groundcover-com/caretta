package main

import (
	"log"

	"github.com/groundcover-com/caretta/pkg/caretta"
)

func main() {
	log.Print("Caretta starting...")
	caretta := caretta.NewCaretta()

	caretta.Start()
	for {

	}
}

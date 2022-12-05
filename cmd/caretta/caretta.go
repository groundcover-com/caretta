package main

import (
	"log"

	"github.com/groundcover-com/caretta/pkg/caretta"
)

func main() {
	log.Print("Caretta starting...")
	caretta := caretta.NewCaretta()
	defer caretta.Stop()

	caretta.Start()
	<-caretta.StopSignal
}

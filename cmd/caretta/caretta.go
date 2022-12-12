package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/groundcover-com/caretta/pkg/caretta"
)

func main() {
	log.Print("Caretta starting...")
	caretta := caretta.NewCaretta()

	caretta.Start()

	osSignal := make(chan os.Signal, 1)
	signal.Notify(osSignal, syscall.SIGINT, syscall.SIGTERM)
	<-osSignal
	caretta.Stop()

}

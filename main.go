package main

import (
	"embed"
	"flag"
	"log"
	"os"

	"github.com/kardianos/service"
)

const (
	nameOfService = "scribble-server"
)

var Version = "dev"

//go:embed tmpl/*.html
var embeddedTemplates embed.FS

//go:embed assets/*
var embeddedAssets embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "backup" {
		// Reads .env like the service does, so SQLITE_PATH and BACKUP_DIR match it.
		loadSettings()
		os.Exit(runBackup(os.Args[2:], settings, os.Stdout, os.Stderr))
	}

	if len(os.Args) > 1 && os.Args[1] == "replica" {
		loadSettings()
		os.Exit(runReplica(os.Args[2:], settings, os.Stdout, os.Stderr))
	}

	svcFlag := flag.String("service", "", "Control the system service.")
	flag.Parse()

	svcConfig := &service.Config{
		Name:        nameOfService,
		DisplayName: nameOfService,
		Description: nameOfService,
	}

	prg := newProgram()
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatal(err)
	}
	errs := make(chan error, 5)
	logger, err = s.Logger(errs)
	if err != nil {
		log.Fatal(err)
	}

	go func() {
		for {
			err := <-errs
			if err != nil {
				log.Print(err)
			}
		}
	}()

	if len(*svcFlag) != 0 {
		err := service.Control(s, *svcFlag)
		if err != nil {
			log.Printf("Valid actions: %q\n", service.ControlAction)
			log.Fatal(err)
		}
		return
	}
	// If the HTTP server stops on its own, exit non-zero so the service
	// manager restarts the service; Run would otherwise keep waiting for a
	// stop signal while nothing is serving.
	go func() {
		err := <-prg.failed
		logger.Errorf("service stopped unexpectedly: %v", err)
		os.Exit(1)
	}()

	err = s.Run()
	if err != nil {
		// A failed start must not look like a clean stop to service
		// managers and restart policies.
		logger.Error(err)
		os.Exit(1)
	}
}

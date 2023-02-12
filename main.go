package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	config, err := getImmutableConfigs()
	if err != nil {
		log.Fatalf("Error getting configuring: %s", err.Error())
	}

	err = profileLoop(config)
	if err != nil {
		log.Fatalf("Error with profiles: %s", err.Error())
	}
}

func profileLoop(c *Configurations) error {
	profiles, err := c.getProfiles()
	if err != nil {
		return fmt.Errorf("getting inital profiles: %w", err)
	}

	if len(profiles) < 1 {
		log.Fatalf("Nothing to run")
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGHUP)

	hold(profiles)

	for {
		// reloads
		<-sig

		//
	}
}

func hold(x ...any) {}

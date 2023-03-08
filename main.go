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
	var insts []*Instance
	profiles, err := c.getProfiles()
	if err != nil {
		return fmt.Errorf("getting inital profiles: %w", err)
	}

	if len(profiles) < 1 {
		log.Fatalf("Nothing to run")
	}

	insts = make([]*Instance, len(profiles))
	for i, p := range profiles {
		if err := p.Resolve(); err != nil {
			log.Fatalf("Error reading files for profile %q: %s", p.Name, err)
		}

		inst, err := NewInstance(p)
		if err != nil {
			log.Fatalf("Error inilizing %q: %s", p.Name, err)
		}
		insts[i] = inst
	}

	sig := make(chan os.Signal)
	signal.Notify(sig, syscall.SIGHUP)

	for {
		<-sig // reload

		np, err := c.getProfiles()
		if err != nil {
			log.Println("Failed to reload profiles: " + err.Error())
			continue
		}

		var abort bool
		removeInst := make([]*Instance, len(insts))
		modifyInst := make([]struct {
			P *Profile
			I *Instance
		}, 0, len(insts))
		addInst := make([]*Profile, 0, len(insts))
		copy(removeInst, insts)

		for _, p := range np {
			if err := p.Resolve(); err != nil {
				log.Println(fmt.Sprintf("Error reading files for profile %q: %s", p.Name, err))
				abort = true
				break
			}

			var found bool
			for i := 0; i < len(removeInst); {
				if p.Name != removeInst[i].p.Name {
					i++
					continue
				}

				found = true
				modifyInst = append(modifyInst, struct {
					P *Profile
					I *Instance
				}{P: p, I: removeInst[i]})
				removeInst[i] = removeInst[len(removeInst)-1]
				removeInst = removeInst[:len(removeInst)-1]
				break
			}

			if !found {
				addInst = append(addInst, p)
			}
		}

		if abort {
			continue
		}

		for _, i := range removeInst {
			if Debug {
				log.Println(fmt.Sprintf("Removing %q", i.p.Name))
			}
			i.Stop()

			for ii := 0; ii < len(insts); ii++ {
				if i == insts[ii] {
					insts[ii] = insts[len(insts)-1]
					insts = insts[:len(insts)-1]
					break
				}
			}
		}

		for _, m := range modifyInst {
			if err := m.I.AdaptTo(m.P); err != nil {
				log.Println(fmt.Sprintf("Error modifying profile %q: %s", m.P.Name, err))
			} else if Debug {
				log.Println(fmt.Sprintf("Reloaded %q", m.P.Name))
			}
		}

		for _, p := range addInst {
			i, err := NewInstance(p)
			if err != nil {
				log.Println(fmt.Sprintf("Error adding profile %q: %s", p.Name, err))
				continue
			} else if Debug {
				log.Println(fmt.Sprintf("Added %q", p.Name))
			}
			insts = append(insts, i)
		}
	}
}

package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

type resetCmd struct {
	Force bool `help:"Skip confirmation prompt." default:"false"`
}

func (c *resetCmd) Run(app *cli) error {
	if !c.Force {
		fmt.Print("Reset device? This will stop all operations. [y/N] ")
		reader := bufio.NewReader(os.Stdin)
		answer, _ := reader.ReadString('\n')
		answer = strings.TrimSpace(strings.ToLower(answer))
		if answer != "y" && answer != "yes" {
			fmt.Println("Aborted.")
			return nil
		}
	}

	dev, err := openDevice(app)
	if err != nil {
		return err
	}
	defer dev.Close()

	if err := dev.Reset(); err != nil {
		return fmt.Errorf("reset: %w", err)
	}
	fmt.Println("Device reset.")
	return nil
}

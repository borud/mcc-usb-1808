package main

import (
	"fmt"
	"runtime"
)

type versionCmd struct {
	Format string `help:"Output format (${enum})." default:"text" enum:"text,json"`
}

func (c *versionCmd) Run(app *cli) error {
	if c.Format == "json" {
		return printJSON(map[string]string{
			"version":    version,
			"build_date": buildDate,
			"go_version": runtime.Version(),
			"os":         runtime.GOOS,
			"arch":       runtime.GOARCH,
		})
	}

	fmt.Printf("daq %s\n", version)
	fmt.Printf("Built:  %s\n", buildDate)
	fmt.Printf("Go:     %s\n", runtime.Version())
	fmt.Printf("OS:     %s/%s\n", runtime.GOOS, runtime.GOARCH)
	return nil
}

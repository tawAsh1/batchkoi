package batchkoi

import "fmt"

type VersionCmd struct{}

func (c *VersionCmd) Run(app *App) error {
	fmt.Printf("batchkoi %s\n", version)
	return nil
}

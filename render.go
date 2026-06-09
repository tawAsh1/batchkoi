package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
)

type RenderCmd struct{}

func (c *RenderCmd) Run(app *App) error {
	if err := app.setup(); err != nil {
		return err
	}
	src, err := app.renderJobDefinition()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, src, "", "  "); err != nil {
		// Not valid JSON — print the raw output so the user can see what went wrong.
		fmt.Fprintln(os.Stdout, string(src))
		return nil
	}
	fmt.Fprintln(os.Stdout, buf.String())
	return nil
}

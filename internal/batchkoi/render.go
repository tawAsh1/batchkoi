package batchkoi

import (
	"bytes"
	"encoding/json"
	"fmt"
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
		// Not valid JSON — print the raw output so the user can see what
		// went wrong, but fail so CI catches it.
		fmt.Fprintln(app.out(), string(src))
		return fmt.Errorf("rendered %s is not valid JSON: %w", app.config.JobDefinition, err)
	}
	fmt.Fprintln(app.out(), buf.String())
	return nil
}

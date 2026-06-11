package batchkoi

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// result is a command outcome that can be rendered as text or JSON.
type result interface {
	String() string
}

// out returns the writer command results go to: os.Stdout, unless a test
// injected a buffer.
func (app *App) out() io.Writer {
	if app.stdout != nil {
		return app.stdout
	}
	return os.Stdout
}

// emit prints r as indented JSON when --output json, otherwise as text.
func (app *App) emit(r result) error {
	w := app.out()
	if app.cli.Output == "json" {
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(w, string(b))
		return err
	}
	_, err := fmt.Fprintln(w, r.String())
	return err
}

func joinInts(xs []int32) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.FormatInt(int64(x), 10)
	}
	return strings.Join(parts, ", ")
}

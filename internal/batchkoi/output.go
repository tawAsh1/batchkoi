package batchkoi

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// result is a command outcome that can be rendered as text or JSON.
type result interface {
	String() string
}

// emit prints r as indented JSON when --output json, otherwise as text.
func (app *App) emit(r result) error {
	if app.cli.Output == "json" {
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		_, err = fmt.Fprintln(os.Stdout, string(b))
		return err
	}
	_, err := fmt.Fprintln(os.Stdout, r.String())
	return err
}

func joinInts(xs []int32) string {
	parts := make([]string, len(xs))
	for i, x := range xs {
		parts[i] = strconv.FormatInt(int64(x), 10)
	}
	return strings.Join(parts, ", ")
}

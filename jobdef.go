package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/aws/aws-sdk-go-v2/service/batch"
	jsonnet "github.com/google/go-jsonnet"
)

// renderJobDefinition evaluates the job definition file (Jsonnet or JSON) with
// batchkoi's native functions and returns JSON bytes.
func (app *App) renderJobDefinition() ([]byte, error) {
	path := app.config.JobDefinition
	if strings.HasSuffix(path, ".json") {
		// TODO: Go text/template support (ecspresso parity) for .json files.
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("failed to read %s: %w", path, err)
		}
		return b, nil
	}

	vm := jsonnet.MakeVM()
	for k, v := range app.cli.ExtStr {
		vm.ExtVar(k, v)
	}
	for k, v := range app.cli.ExtCode {
		vm.ExtCode(k, v)
	}
	funcs, err := app.nativeFuncs()
	if err != nil {
		return nil, err
	}
	for _, f := range funcs {
		vm.NativeFunction(f)
	}
	out, err := vm.EvaluateFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to evaluate %s: %w", path, err)
	}
	return []byte(out), nil
}

// loadJobDefinition renders the config and decodes it into a
// RegisterJobDefinitionInput. The JSON mirrors the AWS Batch API shape 1:1.
func (app *App) loadJobDefinition() (*batch.RegisterJobDefinitionInput, error) {
	src, err := app.renderJobDefinition()
	if err != nil {
		return nil, err
	}
	var in batch.RegisterJobDefinitionInput
	dec := json.NewDecoder(bytes.NewReader(src))
	if err := dec.Decode(&in); err != nil {
		return nil, fmt.Errorf("failed to parse job definition %s: %w", app.config.JobDefinition, err)
	}
	return &in, nil
}

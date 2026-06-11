package main

import (
	"fmt"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/fujiwara/tfstate-lookup/tfstate"
	jsonnet "github.com/google/go-jsonnet"
	"github.com/google/go-jsonnet/ast"
)

// nativeFuncs returns the Jsonnet native functions available to the job
// definition template, mirroring ecspresso: env/must_env/caller_identity are
// always present; tfstate/ssm are enabled by declaring the matching plugin in
// batchkoi.yml.
//
// In Jsonnet they are reached via std.native, e.g.:
//
//	local env = std.native('env');
//	local tfstate = std.native('tfstate');
//	local account = std.native('caller_identity')().Account;
func (app *App) nativeFuncs() ([]*jsonnet.NativeFunction, error) {
	funcs := []*jsonnet.NativeFunction{envFunc(), mustEnvFunc(), app.callerIdentityFunc()}

	for _, p := range app.config.Plugins {
		switch p.Name {
		case "tfstate":
			loc := p.Config["path"]
			if loc == "" {
				loc = p.Config["url"]
			}
			if loc == "" {
				return nil, fmt.Errorf("tfstate plugin: 'path' or 'url' is required")
			}
			tf, err := tfstate.JsonnetNativeFuncs(app.ctx, "", app.config.resolve(loc))
			if err != nil {
				return nil, fmt.Errorf("tfstate plugin: %w", err)
			}
			funcs = append(funcs, tf...)
		case "ssm":
			funcs = append(funcs, app.ssmFunc())
		default:
			return nil, fmt.Errorf("unknown plugin %q", p.Name)
		}
	}
	return funcs, nil
}

func envFunc() *jsonnet.NativeFunction {
	return &jsonnet.NativeFunction{
		Name:   "env",
		Params: ast.Identifiers{"name", "default"},
		Func: func(args []interface{}) (interface{}, error) {
			name, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("env: name must be a string")
			}
			if v, ok := os.LookupEnv(name); ok {
				return v, nil
			}
			return args[1], nil
		},
	}
}

func mustEnvFunc() *jsonnet.NativeFunction {
	return &jsonnet.NativeFunction{
		Name:   "must_env",
		Params: ast.Identifiers{"name"},
		Func: func(args []interface{}) (interface{}, error) {
			name, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("must_env: name must be a string")
			}
			v, ok := os.LookupEnv(name)
			if !ok {
				return nil, fmt.Errorf("must_env: environment variable %s is not set", name)
			}
			return v, nil
		},
	}
}

// callerIdentityFunc exposes sts:GetCallerIdentity to templates, like
// ecspresso. Returns {Account, Arn, UserId}; the call is lazy and cached, so
// templates that don't use it never hit STS.
func (app *App) callerIdentityFunc() *jsonnet.NativeFunction {
	return &jsonnet.NativeFunction{
		Name:   "caller_identity",
		Params: ast.Identifiers{},
		Func: func(args []interface{}) (interface{}, error) {
			id, err := app.callerIdentity()
			if err != nil {
				return nil, fmt.Errorf("caller_identity: %w", err)
			}
			return map[string]interface{}{
				"Account": aws.ToString(id.Account),
				"Arn":     aws.ToString(id.Arn),
				"UserId":  aws.ToString(id.UserId),
			}, nil
		},
	}
}

func (app *App) ssmFunc() *jsonnet.NativeFunction {
	return &jsonnet.NativeFunction{
		Name:   "ssm",
		Params: ast.Identifiers{"name"},
		Func: func(args []interface{}) (interface{}, error) {
			name, ok := args[0].(string)
			if !ok {
				return nil, fmt.Errorf("ssm: name must be a string")
			}
			out, err := ssm.NewFromConfig(app.awsCfg).GetParameter(app.ctx, &ssm.GetParameterInput{
				Name:           aws.String(name),
				WithDecryption: aws.Bool(true),
			})
			if err != nil {
				return nil, fmt.Errorf("ssm: get %s: %w", name, err)
			}
			return aws.ToString(out.Parameter.Value), nil
		},
	}
}

package main

import "errors"

// errNotImplemented is returned by commands that are scaffolded but not built yet.
var errNotImplemented = errors.New("not implemented yet — coming soon \U0001F3A3")

type VerifyCmd struct{}

func (c *VerifyCmd) Run(app *App) error { return errNotImplemented }

type StatusCmd struct{}

func (c *StatusCmd) Run(app *App) error { return errNotImplemented }

type InitCmd struct{}

func (c *InitCmd) Run(app *App) error { return errNotImplemented }

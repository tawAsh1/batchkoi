package batchkoi

type VersionCmd struct{}

// VersionResult is the outcome of `batchkoi version`.
type VersionResult struct {
	Version string `json:"version"`
}

func (r VersionResult) String() string { return "batchkoi " + r.Version }

func (c *VersionCmd) Run(app *App) error {
	return app.emit(VersionResult{Version: version})
}

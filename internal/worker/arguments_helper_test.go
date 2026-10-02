package worker

// arguments exposes the CLI argv a cliAdapter would build for the worker's
// current config. It exists for tests exercising exec-based adapters; a
// non-cliAdapter (e.g. an ACP-based one) has no argv to expose.
func (w *Worker) arguments() []string {
	if cli, ok := w.adapter.(cliAdapter); ok {
		return cli.Arguments(w.config)
	}
	return nil
}

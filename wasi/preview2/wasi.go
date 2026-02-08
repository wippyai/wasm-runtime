package preview2

// WASI configures a WASI preview2 environment. Use builder methods to set up.
type WASI struct {
	resources *ResourceTable
	stdin     *InputStreamResource
	stdout    *OutputStreamResource
	stderr    *OutputStreamResource
	env       map[string]string
	preopens  map[string]string
	cwd       string
	args      []string
}

// New creates a new WASI preview2 instance
func New() *WASI {
	w := &WASI{
		resources: NewResourceTable(),
		stdin:     NewInputStreamResource(nil),
		stdout:    NewOutputStreamResource(nil),
		stderr:    NewOutputStreamResource(nil),
		env:       make(map[string]string),
		args:      nil,
		cwd:       "/",
		preopens:  make(map[string]string),
	}
	return w
}

// WithEnv sets environment variables
func (w *WASI) WithEnv(env map[string]string) *WASI {
	w.env = env
	return w
}

// WithArgs sets command-line arguments
func (w *WASI) WithArgs(args []string) *WASI {
	w.args = args
	return w
}

// WithCwd sets the current working directory
func (w *WASI) WithCwd(cwd string) *WASI {
	w.cwd = cwd
	return w
}

// WithPreopens sets preopened directories
func (w *WASI) WithPreopens(preopens map[string]string) *WASI {
	w.preopens = preopens
	return w
}

// WithStdin sets stdin data
func (w *WASI) WithStdin(data []byte) *WASI {
	w.stdin = NewInputStreamResource(data)
	return w
}

// Stdout returns stdout contents
func (w *WASI) Stdout() []byte {
	return w.stdout.Bytes()
}

// Stderr returns stderr contents
func (w *WASI) Stderr() []byte {
	return w.stderr.Bytes()
}

// Resources returns the resource table
func (w *WASI) Resources() *ResourceTable {
	return w.resources
}

// Env returns environment variables
func (w *WASI) Env() map[string]string {
	return w.env
}

// Args returns command-line arguments
func (w *WASI) Args() []string {
	return w.args
}

// Cwd returns current working directory
func (w *WASI) Cwd() string {
	return w.cwd
}

// Preopens returns preopened directories
func (w *WASI) Preopens() map[string]string {
	return w.preopens
}

// Stdin returns stdin resource
func (w *WASI) Stdin() *InputStreamResource {
	return w.stdin
}

// StdoutResource returns stdout resource
func (w *WASI) StdoutResource() *OutputStreamResource {
	return w.stdout
}

// StderrResource returns stderr resource
func (w *WASI) StderrResource() *OutputStreamResource {
	return w.stderr
}

// Close cleans up all resources
func (w *WASI) Close() {
	w.resources.Clear()
}

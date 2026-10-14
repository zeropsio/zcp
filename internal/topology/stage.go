package topology

// Stage is one stage runtime of a dev/stage pair: a service built from the
// dev half's repository with its own zerops.yml setup. A pair holds one dev
// service and any number of stages, one per part of the app; a single-part
// app has exactly one. Setup is empty until it is known —
// a recipe plan names it, a deploy or set-default-setup records it.
type Stage struct {
	Hostname string `json:"hostname"`
	Setup    string `json:"setup,omitempty"`
}

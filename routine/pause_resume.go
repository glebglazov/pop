package routine

// PauseResult is the outcome of pausing a Routine.
type PauseResult struct {
	RoutineID     string
	AlreadyPaused bool
}

// Pause sets a Routine's persisted pause bit using default dependencies.
func Pause(id string) (*PauseResult, error) {
	return PauseWith(defaultDeps, id)
}

// PauseWith suspends scheduled firing for a Routine without deleting it.
func PauseWith(d *Deps, id string) (*PauseResult, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	r, err := loadManifest(d, id)
	if err != nil {
		return nil, err
	}
	if r.Manifest.Paused {
		return &PauseResult{RoutineID: id, AlreadyPaused: true}, nil
	}
	r.Manifest.Paused = true
	if err := writeManifest(d, id, r.Manifest); err != nil {
		return nil, err
	}
	return &PauseResult{RoutineID: id}, nil
}

// ResumeResult is the outcome of resuming a Routine.
type ResumeResult struct {
	RoutineID   string
	NotPaused   bool
}

// Resume clears a Routine's persisted pause bit using default dependencies.
func Resume(id string) (*ResumeResult, error) {
	return ResumeWith(defaultDeps, id)
}

// ResumeWith re-enables scheduled firing for a paused Routine.
func ResumeWith(d *Deps, id string) (*ResumeResult, error) {
	if err := validateID(id); err != nil {
		return nil, err
	}
	r, err := loadManifest(d, id)
	if err != nil {
		return nil, err
	}
	if !r.Manifest.Paused {
		return &ResumeResult{RoutineID: id, NotPaused: true}, nil
	}
	r.Manifest.Paused = false
	if err := writeManifest(d, id, r.Manifest); err != nil {
		return nil, err
	}
	return &ResumeResult{RoutineID: id}, nil
}

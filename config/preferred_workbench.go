package config

// This file is how a Preferred workbench is stated and read back outside the
// resolution chain: `pop workbench prefer` states one, and the picker it opens
// asks what this repository already says.
//
// Choosing a Workbench to come back to is a human stating intent, so it lands in
// the override layer's block for the repository (ADR-0212 decisions 5 and 6),
// not in a per-checkout record pop kept for itself. The key has no global home,
// so the repository is the only scope that can hold it — which is also why every
// worktree of a repository now reads one answer instead of inheriting the Trunk
// worktree's entry.
//
// `pop workbench prefer` and the Config dashboard's repo.preferred_workbench row
// are two front-ends over this one destination (decision 6): the chord that was
// the third is gone.

// preferredWorkbenchKey is the repo-block leaf the two front-ends write.
const preferredWorkbenchKey = "preferred_workbench"

// StatePreferredWorkbench states name as the Preferred workbench of the
// repository owning checkoutPath. An empty name is the explicit-none sentinel
// ("no workbench here"), which is a stated value and not an absent one: it is
// the entry's presence, not its emptiness, that decides (ADR-0078's three-valued
// entry, kept).
func StatePreferredWorkbench(checkoutPath, name string) error {
	return StatePreferredWorkbenchWith(defaultDeps, checkoutPath, name)
}

// StatePreferredWorkbenchWith is the injectable variant.
func StatePreferredWorkbenchWith(d *Deps, checkoutPath, name string) error {
	_, err := SetRepoOverrideValueWith(d, checkoutPath, preferredWorkbenchKey, name)
	return err
}

// ClearPreferredWorkbench removes the repository's stated Preferred workbench,
// so what the layers below resolve — a committed .pop/config.toml, a
// [repo."<path>"] block, or nothing at all — is in force again. Clearing what
// was never stated is a no-op.
func ClearPreferredWorkbench(checkoutPath string) error {
	return ClearPreferredWorkbenchWith(defaultDeps, checkoutPath)
}

// ClearPreferredWorkbenchWith is the injectable variant.
func ClearPreferredWorkbenchWith(d *Deps, checkoutPath string) error {
	return DeleteRepoOverrideValueWith(d, checkoutPath, preferredWorkbenchKey)
}

// StatedPreferredWorkbench reports what the override layer states as the
// Preferred workbench of the repository owning checkoutPath. The bool is the
// three-valued part: false is "nothing stated here" and true with an empty name
// is an explicit none, which is what the picker's "<reset>" entry exists to
// undo.
func StatedPreferredWorkbench(checkoutPath string) (string, bool, error) {
	return StatedPreferredWorkbenchWith(defaultDeps, checkoutPath)
}

// StatedPreferredWorkbenchWith is the injectable variant.
func StatedPreferredWorkbenchWith(d *Deps, checkoutPath string) (string, bool, error) {
	value, ok, err := RepoOverrideValueWith(d, checkoutPath, preferredWorkbenchKey)
	if err != nil || !ok {
		return "", false, err
	}
	name, _ := value.(string)
	return name, true, nil
}

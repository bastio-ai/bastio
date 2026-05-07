package overlay

import "github.com/google/uuid"

// Identity names a specific overlay version. Carried into logs / audit
// trail so the overlay responsible for a runtime effect (especially a
// loosened override) can be found quickly without cross-referencing
// other data.
//
// Lives in this package — rather than security — so the overlay package
// stays free of any import from security. Apply (see security package)
// takes an Identity to attribute loosening warnings.
type Identity struct {
	OverlayID uuid.UUID
	VersionID uuid.UUID
	Version   int
}

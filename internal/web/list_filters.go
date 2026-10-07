package web

// listFilterOpt is one option in a list_filters select.
type listFilterOpt struct {
	Value, Label string
	Selected     bool
	Href         string // select: click applies this value
	Icon         string // optional Lucide icon name for menu / trigger
}

// listFilterSelect is one Filter-menu field (or a legacy list_filters select).
type listFilterSelect struct {
	Name, AriaLabel        string
	Options                []listFilterOpt
	SelectedCount          int    // selected option rows; accordion summary badge when > 0
	PresenceField          string // library.Presence* id; empty = no Has/No
	PresenceEmptyHref      string
	PresenceFilledHref     string
	PresenceEmptyLabel     string
	PresenceFilledLabel    string
	PresenceEmptySelected  bool
	PresenceFilledSelected bool
}

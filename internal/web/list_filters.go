package web

// listFilterOpt is one option in a list_filters select.
type listFilterOpt struct {
	Value, Label string
	Selected     bool
}

// listFilterSelect is one <select> in list_filters.
type listFilterSelect struct {
	Name, AriaLabel, EmptyLabel string
	Options                     []listFilterOpt
}

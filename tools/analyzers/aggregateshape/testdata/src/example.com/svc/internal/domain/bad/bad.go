package bad

type OrderID string

// A string id is not a typed id: it can be mixed up with any other string.
//
//agent:aggregate
type PlainID struct { // want `aggregate PlainID has no typed id`
	id string
}

func NewPlainID(id string) PlainID { return PlainID{id: id} }

func RestorePlainID(id string) PlainID { return PlainID{id: id} }

// No id field at all.
//
//agent:aggregate
type Anonymous struct { // want `aggregate Anonymous has no typed id`
	name string
}

func NewAnonymous() Anonymous { return Anonymous{} }

func RestoreAnonymous() Anonymous { return Anonymous{} }

//agent:aggregate
type NoNew struct { // want `aggregate NoNew has no constructor NewNoNew`
	id OrderID
}

func RestoreNoNew(id OrderID) NoNew { return NoNew{id: id} }

//agent:aggregate
type NoRestore struct { // want `aggregate NoRestore has no RestoreNoRestore`
	id OrderID
}

func NewNoRestore(id OrderID) NoRestore { return NoRestore{id: id} }

//agent:aggregate
type WrongResult struct { // want `aggregate WrongResult has no constructor NewWrongResult`
	id OrderID
}

func NewWrongResult(id OrderID) string { return string(id) }

func RestoreWrongResult(id OrderID) WrongResult { return WrongResult{id: id} }

//agent:aggregate
type Everything struct { // want `aggregate Everything has no typed id` `aggregate Everything has no constructor NewEverything` `aggregate Everything has no RestoreEverything`
	label string
}

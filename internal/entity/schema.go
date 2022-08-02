package entity

type DatasetIdentifier struct {
	Owner string `json:"owner"`
	Name  string `json:"name"`
}

type Schema struct {
	Owner      string       `json:"owner"`
	Name       string       `json:"name"`
	Tables     []*Table     `json:"tables"`
	Actions    []*Action    `json:"actions"`
	Extensions []*Extension `json:"extensions"`
}

type Extension struct {
	Name   string            `json:"name"`
	Config map[string]string `json:"config"`
	Alias  string            `json:"alias"`
}

type Table struct {
	Name        string        `json:"name"`
	Columns     []*Column     `json:"columns"`
	Indexes     []*Index      `json:"indexes"`
	ForeignKeys []*ForeignKey `json:"foreign_keys"`
}

type Column struct {
	Name       string       `json:"name"`
	Type       string       `json:"type"`
	Attributes []*Attribute `json:"attributes,omitempty"`
}

type Attribute struct {
	Type  string `json:"type"`
	Value any    `json:"value"`
}

type Action struct {
	Name       string   `json:"name"`
	Inputs     []string `json:"inputs"`
	Public     bool     `json:"public"`
	Statements []string `json:"statements"`
}

type Index struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Type    string   `json:"type"`
}

type ForeignKey struct {
	// ChildKeys are the columns that are referencing another.
	// For example, in FOREIGN KEY (a) REFERENCES tbl2(b), "a" is the child key
	ChildKeys []string `json:"child_keys"`

	// ParentKeys are the columns that are being referred to.
	// For example, in FOREIGN KEY (a) REFERENCES tbl2(b), "tbl2.b" is the parent key
	ParentKeys []string `json:"parent_keys"`

	// ParentTable is the table that holds the parent columns.
	// For example, in FOREIGN KEY (a) REFERENCES tbl2(b), "tbl2.b" is the parent table
	ParentTable string `json:"parent_table"`

	// Action refers to what the foreign key should do when the parent is altered.
	// This is NOT the same as a database action;
	// however sqlite's docs refer to these as actions,
	// so we should be consistent with that.
	// For example, ON DELETE CASCADE is a foreign key action
	Actions []*ForeignKeyAction `json:"actions"`
}

// ForeignKeyAction is used to specify what should occur
// if a parent key is updated or deleted
type ForeignKeyAction struct {
	// On can be either "UPDATE" or "DELETE"
	On string `json:"on"`

	// Do specifies what a foreign key action should do
	Do string `json:"do"`
}

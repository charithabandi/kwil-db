package transactions

import (
	"github.com/kwilteam/kwil-db/pkg/serialize"
)

// PayloadType is the type of payload
type PayloadType string

func (p PayloadType) String() string {
	return string(p)
}

func (p PayloadType) Valid() bool {
	switch p {
	case PayloadTypeDeploySchema,
		PayloadTypeDropSchema,
		PayloadTypeExecuteAction,
		PayloadTypeCallAction:
		return true
	default:
		return false
	}
}

const (
	PayloadTypeDeploySchema     PayloadType = "deploy_schema"
	PayloadTypeDropSchema       PayloadType = "drop_schema"
	PayloadTypeExecuteAction    PayloadType = "execute_action"
	PayloadTypeCallAction       PayloadType = "call_action"
	PayloadTypeValidatorJoin    PayloadType = "validator_join"
	PayloadTypeValidatorApprove PayloadType = "validator_approve"
)

// Payload is the interface that all payloads must implement
// Implementations should use Kwil's serialization package to encode and decode themselves
type Payload interface {
	MarshalBinary() (serialize.SerializedData, error)
	UnmarshalBinary(serialize.SerializedData) error
	Type() PayloadType
}

// Schema is the payload that is used to deploy a schema
type Schema struct {
	Owner      string
	Name       string
	Tables     []*Table
	Actions    []*Action
	Extensions []*Extension
}

var _ Payload = (*Schema)(nil)

func (s *Schema) MarshalBinary() (serialize.SerializedData, error) {
	return serialize.Encode(s)
}

func (s *Schema) UnmarshalBinary(b serialize.SerializedData) error {
	result, err := serialize.Decode[Schema](b)
	if err != nil {
		return err
	}

	*s = *result
	return nil
}

func (s *Schema) Type() PayloadType {
	return PayloadTypeDeploySchema
}

type Extension struct {
	Name   string
	Config []*ExtensionConfig
	Alias  string
}

type ExtensionConfig struct {
	Argument string
	Value    string
}

type Table struct {
	Name        string
	Columns     []*Column
	Indexes     []*Index
	ForeignKeys []*ForeignKey
}

type Column struct {
	Name       string
	Type       string
	Attributes []*Attribute
}

type Attribute struct {
	Type  string
	Value string
}

type Action struct {
	Name   string
	Inputs []string
	// Mutability could be empty if the abi is generated by legacy version of kuneiform,
	// default to "update" for backward compatibility
	Mutability string
	// Auxiliaries are the auxiliary types that are required for the action, specifying extra characteristics of the action
	Auxiliaries []string
	Public      bool
	Statements  []string
}

type Index struct {
	Name    string
	Columns []string
	Type    string
}

type ForeignKey struct {
	// ChildKeys are the columns that are referencing another.
	// For example, in FOREIGN KEY (a) REFERENCES tbl2(b), "a" is the child key
	ChildKeys []string

	// ParentKeys are the columns that are being referred to.
	// For example, in FOREIGN KEY (a) REFERENCES tbl2(b), "tbl2.b" is the parent key
	ParentKeys []string

	// ParentTable is the table that holds the parent columns.
	// For example, in FOREIGN KEY (a) REFERENCES tbl2(b), "tbl2.b" is the parent table
	ParentTable string

	// Action refers to what the foreign key should do when the parent is altered.
	// This is NOT the same as a database action;
	// however sqlite's docs refer to these as actions,
	// so we should be consistent with that.
	// For example, ON DELETE CASCADE is a foreign key action
	Actions []*ForeignKeyAction
}

// ForeignKeyAction is used to specify what should occur
// if a parent key is updated or deleted
type ForeignKeyAction struct {
	// On can be either "UPDATE" or "DELETE"
	On string

	// Do specifies what a foreign key action should do
	Do string
}

// MutabilityType is the type of mutability
type MutabilityType string

func (t MutabilityType) String() string {
	return string(t)
}

const (
	MutabilityUpdate MutabilityType = "update"
	MutabilityView   MutabilityType = "view"
)

// AuxiliaryType is the type of auxiliary
type AuxiliaryType string

func (t AuxiliaryType) String() string {
	return string(t)
}

const (
	// AuxiliaryTypeMustSign is used to specify that an action need signature, it is used for `view` action.
	AuxiliaryTypeMustSign AuxiliaryType = "mustsign"
)

// DropSchema is the payload that is used to drop a schema
type DropSchema struct {
	DBID string
}

var _ Payload = (*DropSchema)(nil)

func (s *DropSchema) MarshalBinary() (serialize.SerializedData, error) {
	return serialize.Encode(s)
}

func (s *DropSchema) UnmarshalBinary(b serialize.SerializedData) error {
	res, err := serialize.Decode[DropSchema](b)
	if err != nil {
		return err
	}

	*s = *res

	return nil
}

func (s *DropSchema) Type() PayloadType {
	return PayloadTypeDropSchema
}

// ActionExecution is the payload that is used to execute an action
type ActionExecution struct {
	DBID      string
	Action    string
	Arguments [][]string
}

var _ Payload = (*ActionExecution)(nil)

func (a *ActionExecution) MarshalBinary() (serialize.SerializedData, error) {
	return serialize.Encode(a)
}

func (s *ActionExecution) UnmarshalBinary(b serialize.SerializedData) error {
	res, err := serialize.Decode[ActionExecution](b)
	if err != nil {
		return err
	}

	*s = *res
	return nil
}

func (a *ActionExecution) Type() PayloadType {
	return PayloadTypeExecuteAction
}

// ActionCall is the payload that is used to call an action
type ActionCall struct {
	DBID      string
	Action    string
	Arguments []string
}

var _ Payload = (*ActionCall)(nil)

func (a *ActionCall) MarshalBinary() (serialize.SerializedData, error) {
	return serialize.Encode(a)
}

func (s *ActionCall) UnmarshalBinary(b serialize.SerializedData) error {
	res, err := serialize.Decode[ActionCall](b)
	if err != nil {
		return err
	}

	*s = *res
	return nil
}

func (a *ActionCall) Type() PayloadType {
	return PayloadTypeCallAction
}

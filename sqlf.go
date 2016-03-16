// Package sqlf assists writing SQL statements.
// It is intended for programmers who are comfortable with
// writing SQL, but would like assistance with the tedious
// process of preparing SELECT, INSERT and UPDATE statements
// for tables that have a large number of columns.
package sqlf

import (
	"bytes"
	"fmt"
	"reflect"
	"strings"
)

func Selectf(format string, args ...interface{}) QueryCommand {
	// TODO: not implemented
	return nil
}

// TableInfo contains enough information about a database table
// to assist with generating SQL strings.
type TableInfo struct {
	Name   string
	Select SelectInfo
	Insert InsertInfo
	Update UpdateInfo

	rowType reflect.Type
	columns []*columnInfo
	dialect Dialect
	alias   string
}

// clone makes a complete, deep copy of the table.
// This is important for taking a copy that can be modified.
func (ti *TableInfo) clone() *TableInfo {
	ti2 := &TableInfo{
		Name:    ti.Name,
		rowType: ti.rowType,
		columns: make([]*columnInfo, len(ti.columns)),
		dialect: ti.dialect,
		alias:   ti.alias,
	}
	// create a clone of all of the columns before cloning
	// anything else.
	for i, ci := range ti.columns {
		ti2.columns[i] = ci.clone(ti2)
	}
	ti2.Select = ti.Select.clone(ti2)
	ti2.Insert = ti.Insert.clone(ti2)
	ti2.Update = ti.Update.clone(ti2)

	return ti2
}

// Table creates a TableInfo with the specified table name
// and schema as defined by the struct that is pointed to
// by row.
//
//This function wil panic if row is not a struct
// or a pointer to a struct. The contents of row
// are ignored, only the structure fields and field tags
// are used.
func Table(name string, row interface{}) *TableInfo {
	ti := &TableInfo{Name: name}

	ti.rowType = reflect.TypeOf(row)
	for ti.rowType.Kind() == reflect.Ptr {
		// derefernce pointer(s)
		ti.rowType = ti.rowType.Elem()
	}
	if ti.rowType.Kind() != reflect.Struct {
		panic("sqlf.Table: expected struct or pointer to struct")
	}

	for i := 0; i < ti.rowType.NumField(); i++ {
		field := ti.rowType.Field(i)

		// For compatibility, use Gorm's tag formats, as they have
		// all the information we need. This means you can interop
		// using Gorm with this package if you like.
		if field.Tag.Get("sql") == "-" {
			// ignore field
			continue
		}
		tagSettings := parseTagSetting(field.Tag)

		ci := &columnInfo{
			table:      ti,
			fieldName:  field.Name,
			fieldIndex: i,
		}

		if value, ok := tagSettings["COLUMN"]; ok {
			ci.columnName = value
		} else {
			ci.columnName = ToDBName(ci.fieldName)
		}
		if _, ok := tagSettings["PRIMARY_KEY"]; ok {
			ci.primaryKey = true
		} else if strings.ToLower(ci.fieldName) == "id" {
			ci.primaryKey = true
		}
		if _, ok := tagSettings["AUTO_INCREMENT"]; ok {
			ci.autoIncrement = true
		}
		ti.columns = append(ti.columns, ci)
	}

	ti.Select.TableName = TableName{clause: clauseSelectColumns, table: ti}
	ti.Select.Columns = ColumnList{clause: clauseSelectColumns, table: ti}.All()
	ti.Insert.TableName = TableName{clause: clauseInsertInto, table: ti}
	ti.Insert.Columns = ColumnList{clause: clauseInsertColumns, table: ti}.Insertable()
	ti.Insert.Values = ColumnList{clause: clauseInsertValues, table: ti}.Insertable()
	ti.Update.TableName = TableName{clause: clauseUpdateTable, table: ti}
	ti.Update.SetColumns = ColumnList{clause: clauseUpdateSet, table: ti}.Updateable()
	ti.Update.WhereColumns = ColumnList{clause: clauseUpdateWhere, table: ti}.PrimaryKey()

	return ti
}

// WithDialect creates a clone of the table with a different dialect.
func (ti *TableInfo) WithDialect(dialect Dialect) *TableInfo {
	ti2 := ti.clone()
	ti2.alias = "" // clear out any alias
	ti2.dialect = dialect
	return ti2
}

// WithAlias creates a clone of the table with the specified alias.
// Any SQL statements produced with this table will include the alias
// name for all references of the table.
// Note that alias should be a valid SQL identier, as it is not quoted
// in any SQL statements produced.
func (ti *TableInfo) WithAlias(alias string) *TableInfo {
	ti2 := ti.clone()
	ti2.alias = alias
	return ti2
}

// Dialect returns the SQL dialect to use with this table.
func (ti *TableInfo) Dialect() Dialect {
	if ti.dialect != nil {
		return ti.dialect
	}
	return defaultDialect()
}

// SelectInfo contains information about a table that can
// be formatted for a SELECT statement or a select clause
// in an INSERT statement.
type SelectInfo struct {
	TableName   TableName
	Columns     ColumnList
	Placeholder Placeholder
}

// clone creates a copy associated with a new table.
// That new table must have been cloned from the original.
func (si SelectInfo) clone(ti *TableInfo) SelectInfo {
	return SelectInfo{
		TableName:   si.TableName.clone(ti),
		Columns:     si.Columns.clone(ti),
		Placeholder: si.Placeholder,
	}
}

// UpdateInfo contains information about a table that
// can be formatted for an UPDATE statement.
type UpdateInfo struct {
	TableName    TableName
	SetColumns   ColumnList
	WhereColumns ColumnList
}

// clone creates a copy associated with a new table.
// That new table must have been cloned from the original.
func (ui UpdateInfo) clone(ti *TableInfo) UpdateInfo {
	return UpdateInfo{
		TableName:    ui.TableName.clone(ti),
		SetColumns:   ui.SetColumns.clone(ti),
		WhereColumns: ui.WhereColumns.clone(ti),
	}
}

// InsertInfo contains information about a table that
// can be formatted in an INSERT statement.
type InsertInfo struct {
	// Table name for use in INSERT INTO clause.
	TableName TableName

	// Columns that should appear in the parentheses
	// after  INSERT INTO table(...). By default these
	// are all columns except for any auto-increment columns.
	Columns ColumnList

	// Placeholders that match the Columns list.
	Values ColumnList
}

// clone creates a copy associated with a new table.
// That new table must have been cloned from the original.
func (ii InsertInfo) clone(ti *TableInfo) InsertInfo {
	return InsertInfo{
		TableName: ii.TableName.clone(ti),
		Columns:   ii.Columns.clone(ti),
		Values:    ii.Values.clone(ti),
	}
}

// ColumnInfo contains enough information about a database column
// to assist with generating SQL strings.
type columnInfo struct {
	fieldName     string
	columnName    string
	table         *TableInfo
	primaryKey    bool
	autoIncrement bool
	version       bool
	fieldIndex    int

	// modified on copies during SQL statement preparation
	placeholder int
}

// clone returns a copy of the columnInfo associated with a different
// table. This method is used as part of cloning an entire TableInfo.
func (ci *columnInfo) clone(ti *TableInfo) *columnInfo {
	ci2 := &columnInfo{}
	*ci2 = *ci
	ci2.table = ti
	return ci2
}

func (ci *columnInfo) hasTableAlias() bool {
	return ci.table.alias != ""
}

func (ci *columnInfo) tableAlias() string {
	return ci.table.alias
}

func (ci *columnInfo) hasColumnAlias() bool {
	return ci.table.alias != ""
}

func (ci *columnInfo) columnAlias() string {
	return ci.table.alias + "_" + ci.columnName
}

// sqlClause represents a specific SQL clause. Column lists
// and table names are represented differently depending on
// which SQL clause they appear in.
type sqlClause int

// All of the different clauses of an SQL statement where columns
// and table names can be found.
const (
	clauseSelectColumns sqlClause = iota
	clauseSelectFrom
	clauseInsertInto
	clauseInsertColumns
	clauseInsertValues
	clauseUpdateTable
	clauseUpdateSet
	// TODO: clauseUpdateFrom -- might just be clauseSelectFrom
	clauseUpdateWhere
)

// isInput identifies whether the SQL clause contains placeholders
// for variable input.
func (c sqlClause) isInput() bool {
	return c == clauseInsertValues ||
		c == clauseUpdateSet ||
		c == clauseUpdateWhere
}

// TableName represents the name of a table for formatting
// in an SQL statement. The format will depend on where the
// table appears in the SQL statement. For example, in a SELECT FROM
// clause, the table may include an alias, but in an INSERT INTO statement
// the table will not have an alias. (TODO: INSERT x INTO x ... FROM x, y, etc)
type TableName struct {
	table  *TableInfo
	clause sqlClause
}

// clone makes a copy of the table name that is associated with
// a new table. That new table must have been cloned from the original.
func (tn TableName) clone(ti *TableInfo) TableName {
	tn2 := tn
	tn2.table = ti
	return tn2
}

// String prints the table name in the appropriate
// form for the part of the SQL statement that this TableName
// applies to. Because TableName implements the Stringer
// interface, it can be formatted using "%s" in fmt.Sprintf.
func (tn TableName) String() string {
	dialect := tn.table.Dialect()
	switch tn.clause {
	case clauseSelectFrom:
		if tn.table.alias != "" {
			return fmt.Sprintf("%s as %s",
				dialect.Quote(tn.table.Name),
				tn.table.alias,
			)
		}
		return dialect.Quote(tn.table.Name)
	case clauseInsertInto, clauseUpdateTable:
		return dialect.Quote(tn.table.Name)
	}
	panic(fmt.Sprintf("invalid clause for table name: %d", tn.clause))
}

// ColumnList represents a list of columns associated
// with a table for use in a specific SQL clause.
//
// Each ColumnList represents a subset of the columns in the
// table. For example a column list for the WHERE clause in
// a row update statement will only contain the columns for
// the primary key. However any ColumnList can return a different
// subset of the columns in the table. For example calling the All
// method on any ColumnList will return a ColumnList with all of the
// columns in the table.
type ColumnList struct {
	table  *TableInfo
	filter func(ci *columnInfo) bool
	clause sqlClause
}

// clone makes a copy of the ColumnList that is associated
// with a different TableInfo. That different TableInfo must
// have been cloned from the original.
func (cil ColumnList) clone(ti *TableInfo) ColumnList {
	return ColumnList{
		table:  ti,
		filter: cil.filter,
		clause: cil.clause,
	}
}

func (cil ColumnList) filtered() []*columnInfo {
	if cil.filter == nil {
		return cil.table.columns
	}
	var list []*columnInfo
	for _, ci := range cil.table.columns {
		if cil.filter(ci) {
			list = append(list, ci)
		}
	}
	return list
}

// All returns a column list of all of the columns in the associated table.
func (cil ColumnList) All() ColumnList {
	return ColumnList{clause: cil.clause, table: cil.table}
}

// Include returns a column list of all columns corresponding
// to the list of names. When specifying columns, use the
// name of field in the Go struct, not the column name in the
// database table.
func (cil ColumnList) Include(names ...string) ColumnList {
	return cil.applyFilter(func(ci *columnInfo) bool {
		for _, name := range names {
			if name == ci.fieldName {
				return true
			}
		}
		return false
	})
}

// Insertable returns a column list of all columns in the associated
// table that can be inserted. This list includes all columns except
// an auto-increment column, if the table has one.
func (cil ColumnList) Insertable() ColumnList {
	return cil.applyFilter(func(ci *columnInfo) bool {
		return !ci.autoIncrement
	})
}

// PrimaryKey returns a column list containing all primary key columns in the
// associated table.
func (cil ColumnList) PrimaryKey() ColumnList {
	return cil.applyFilter(func(ci *columnInfo) bool {
		return ci.primaryKey
	})
}

// String prints the columns in the list in the appropriate
// form for the part of the SQL statement that this column
// list applies to. Because ColumnList implements the fmt.Stringer
// interface, it can be formatted using "%s" in fmt.Sprintf.
func (cil ColumnList) String() string {
	var buf bytes.Buffer
	for i, ci := range cil.filtered() {
		if i > 0 {
			if cil.clause == clauseUpdateWhere {
				buf.WriteString(" and ")
			} else {
				buf.WriteRune(',')
			}
		}
		switch cil.clause {
		case clauseSelectColumns:
			if ci.hasTableAlias() {
				buf.WriteString(ci.tableAlias())
				buf.WriteRune('.')
			}
			buf.WriteString(ci.table.Dialect().Quote(ci.columnName))
			if ci.hasColumnAlias() {
				buf.WriteString(" as ")
				buf.WriteString(ci.columnAlias())
			}
		case clauseInsertColumns:
			buf.WriteString(ci.table.Dialect().Quote(ci.columnName))
		case clauseInsertValues:
			buf.WriteString(ci.table.Dialect().Placeholder(ci.placeholder))
		case clauseUpdateSet, clauseUpdateWhere:
			buf.WriteString(ci.table.Dialect().Quote(ci.columnName))
			buf.WriteRune('=')
			buf.WriteString(ci.table.Dialect().Placeholder(ci.placeholder))
		}
	}
	return buf.String()
}

// Updateable returns a column list of all columns that can be
// updated in the associated table. This list excludes any
// primary key columns and any auto-increment column.
func (cil ColumnList) Updateable() ColumnList {
	return cil.applyFilter(func(ci *columnInfo) bool {
		return !ci.primaryKey && !ci.autoIncrement
	})
}

// apply returns a column list of all columns in the
// table for which the filter function f returns true.
func (cil ColumnList) applyFilter(f func(ci *columnInfo) bool) ColumnList {
	return ColumnList{
		clause: cil.clause,
		table:  cil.table,
		filter: f,
	}
}

// Placeholder represents a placeholder in an SQL query that
// represents a variable that will be bound to the query at
// execution time. Different SQL dialects have varied formats
// for placeholders, but most will accept a single question mark
// ("?"). PostgreSQL is a notable exception as it requires a numberd
// placeholde (eg "$1").
type Placeholder struct {
	table    *TableInfo
	position int
}

func (p Placeholder) String() string {
	return p.table.Dialect().Placeholder(p.position)
}

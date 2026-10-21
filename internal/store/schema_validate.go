package store

import (
	"database/sql"
	"errors"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
)

func validateCurrentSchema(tx *sql.Tx) error {
	tables, err := schemaTableNames(tx)
	if err != nil {
		return err
	}
	for _, table := range currentSchemaTables {
		missing, err := missingSchemaItem(tx, tables, table)
		if err != nil {
			return err
		}
		if missing != "" {
			return errors.New(missing)
		}
		allowedColumns := make(map[string]bool, len(table.columns))
		for _, column := range table.columns {
			allowedColumns[column] = true
		}
		mismatch, err := schemaColumnShapeMismatch(tx, table.name, allowedColumns)
		if err != nil {
			return err
		}
		if mismatch != "" {
			return errors.New(mismatch)
		}
		if err := validateUniqueConstraints(tx, table.name); err != nil {
			return err
		}
		if err := validateTableForeignKeys(tx, table.name); err != nil {
			return err
		}
		if err := validateCheckConstraints(tx, table.name); err != nil {
			return err
		}
		if err := validateConstraintConflictPolicies(tx, table.name); err != nil {
			return err
		}
	}
	if len(tables) != len(currentSchemaTables) {
		return fmt.Errorf("current schema has %d user tables, expected %d", len(tables), len(currentSchemaTables))
	}
	if err := validateNoUnexpectedViewsAndTriggers(tx); err != nil {
		return err
	}
	return validateCurrentSchemaIndexes(tx)
}

func schemaTableNames(tx *sql.Tx) (map[string]bool, error) {
	rows, err := tx.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT GLOB 'sqlite_*'`)
	if err != nil {
		return nil, fmt.Errorf("list database tables: %w", err)
	}
	defer rows.Close()

	names := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, fmt.Errorf("read database table name: %w", err)
		}
		names[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate database tables: %w", err)
	}
	return names, nil
}

func schemaColumnNames(tx *sql.Tx, table string) (map[string]bool, error) {
	// Table names come only from the static schema definitions above.
	rows, err := tx.Query(`PRAGMA table_xinfo(` + table + `)`)
	if err != nil {
		return nil, fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]bool{}
	for rows.Next() {
		var cid, notNull, primaryKey, hidden int
		var name, columnType string
		var defaultValue any
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey, &hidden); err != nil {
			return nil, fmt.Errorf("read %s column: %w", table, err)
		}
		columns[name] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s columns: %w", table, err)
	}
	return columns, nil
}

type schemaColumnInfo struct {
	typeName     string
	notNull      int
	primaryKey   int
	hidden       int
	defaultValue sql.NullString
}

func schemaColumnDetails(tx *sql.Tx, table string) (map[string]schemaColumnInfo, error) {
	// Table names come only from the static schema definitions above.
	rows, err := tx.Query(`PRAGMA table_xinfo(` + table + `)`)
	if err != nil {
		return nil, fmt.Errorf("inspect %s columns: %w", table, err)
	}
	defer rows.Close()

	columns := map[string]schemaColumnInfo{}
	for rows.Next() {
		var cid, notNull, primaryKey, hidden int
		var name, columnType string
		var defaultValue sql.NullString
		if err := rows.Scan(&cid, &name, &columnType, &notNull, &defaultValue, &primaryKey, &hidden); err != nil {
			return nil, fmt.Errorf("read %s column definition: %w", table, err)
		}
		columns[name] = schemaColumnInfo{
			typeName: columnType, notNull: notNull, primaryKey: primaryKey, hidden: hidden, defaultValue: defaultValue,
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s column definitions: %w", table, err)
	}
	return columns, nil
}

func schemaColumnCollations(tx *sql.Tx, table string) (map[string]string, error) {
	var definition string
	if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&definition); err != nil {
		return nil, fmt.Errorf("read %s table definition for collations: %w", table, err)
	}
	segments, err := splitSQLTableDefinitions(definition)
	if err != nil {
		return nil, fmt.Errorf("read %s column definitions: %w", table, err)
	}
	collations := make(map[string]string, len(segments))
	for _, segment := range segments {
		name, rest, ok, err := readSQLIdentifier(segment, 0)
		if err != nil {
			return nil, fmt.Errorf("read %s column name: %w", table, err)
		}
		if !ok {
			continue
		}
		collation, err := sqlColumnCollation(segment[rest:])
		if err != nil {
			return nil, fmt.Errorf("read %s.%s collation: %w", table, name, err)
		}
		collations[name] = collation
	}
	return collations, nil
}

func schemaColumnShapeMismatch(tx *sql.Tx, table string, allowed map[string]bool) (string, error) {
	optionsMismatch, err := schemaTableOptionsMismatch(tx, table)
	if err != nil || optionsMismatch != "" {
		return optionsMismatch, err
	}
	columns, err := schemaColumnDetails(tx, table)
	if err != nil {
		return "", err
	}
	collations, err := schemaColumnCollations(tx, table)
	if err != nil {
		return "", err
	}
	primaryKeys := schemaPrimaryKeys[table]
	columnNames := make([]string, 0, len(columns))
	for name := range columns {
		columnNames = append(columnNames, name)
	}
	sort.Strings(columnNames)
	for _, name := range columnNames {
		column := columns[name]
		if !allowed[name] {
			return fmt.Sprintf("unexpected column %s.%s is present", table, name), nil
		}
		gotCollation, exists := collations[name]
		if !exists {
			return fmt.Sprintf("column %s.%s is missing from its table definition", table, name), nil
		}
		wantCollation := expectedSchemaCollation(table, name)
		if gotCollation != wantCollation {
			return fmt.Sprintf("column %s.%s has collation %q, expected %q", table, name, gotCollation, wantCollation), nil
		}
		if column.hidden != 0 {
			return fmt.Sprintf("column %s.%s is generated or hidden (hidden=%d)", table, name, column.hidden), nil
		}
		wantType := "TEXT"
		if integerSchemaColumns[table][name] {
			wantType = "INTEGER"
		}
		if gotType := strings.ToUpper(strings.TrimSpace(column.typeName)); gotType != wantType {
			return fmt.Sprintf("column %s.%s has type %q, expected %q", table, name, column.typeName, wantType), nil
		}
		wantDefault, hasDefault := schemaColumnDefaults[table][name]
		if column.defaultValue.Valid != hasDefault || (hasDefault && column.defaultValue.String != wantDefault) {
			gotDefault := "<none>"
			if column.defaultValue.Valid {
				gotDefault = column.defaultValue.String
			}
			if !hasDefault {
				wantDefault = "<none>"
			}
			return fmt.Sprintf("column %s.%s has default %q, expected %q", table, name, gotDefault, wantDefault), nil
		}
		wantPrimaryKey := 0
		for index, primaryKey := range primaryKeys {
			if primaryKey == name {
				wantPrimaryKey = index + 1
				break
			}
		}
		if column.primaryKey != wantPrimaryKey {
			return fmt.Sprintf("column %s.%s has primary-key position %d, expected %d", table, name, column.primaryKey, wantPrimaryKey), nil
		}
		if !nullableSchemaColumns[table][name] && !implicitNotNullPrimaryKeyColumns[table][name] && column.notNull != 1 {
			return fmt.Sprintf("column %s.%s must be NOT NULL", table, name), nil
		}
	}
	for _, primaryKey := range primaryKeys {
		if _, exists := columns[primaryKey]; !exists {
			return fmt.Sprintf("primary-key column %s.%s is missing", table, primaryKey), nil
		}
	}
	return "", nil
}

func schemaTableOptionsMismatch(tx *sql.Tx, table string) (string, error) {
	var withoutRowID, strict int
	err := tx.QueryRow(`SELECT wr, strict FROM pragma_table_list WHERE schema = 'main' AND name = ? AND type = 'table'`, table).Scan(&withoutRowID, &strict)
	if err == sql.ErrNoRows {
		return fmt.Sprintf("table %q is missing from SQLite's table list", table), nil
	}
	if err != nil {
		return "", fmt.Errorf("inspect %s table options: %w", table, err)
	}
	if withoutRowID != 0 || strict != 0 {
		return fmt.Sprintf("table %q has noncanonical options without_rowid=%t strict=%t", table, withoutRowID != 0, strict != 0), nil
	}
	return "", nil
}

func schemaKeyColumns(tx *sql.Tx, indexName string) ([]schemaIndexKeyColumn, error) {
	rows, err := tx.Query(`SELECT name, "desc", coll, key FROM pragma_index_xinfo(?) ORDER BY seqno`, indexName)
	if err != nil {
		return nil, fmt.Errorf("inspect index %q columns: %w", indexName, err)
	}
	defer rows.Close()

	columns := []schemaIndexKeyColumn{}
	for rows.Next() {
		var name, collation sql.NullString
		var descending, key int
		if err := rows.Scan(&name, &descending, &collation, &key); err != nil {
			return nil, fmt.Errorf("read index %q column: %w", indexName, err)
		}
		if key == 0 {
			continue
		}
		if !name.Valid || !collation.Valid {
			return nil, fmt.Errorf("index %q has an unreadable key column", indexName)
		}
		columns = append(columns, schemaIndexKeyColumn{
			name: name.String, collation: strings.ToUpper(collation.String), descending: descending != 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate index %q columns: %w", indexName, err)
	}
	return columns, nil
}

func expectedSchemaCollation(table, column string) string {
	if table == "terms" && column == "name" {
		return "NOCASE"
	}
	return "BINARY"
}

func validateUniqueConstraints(tx *sql.Tx, table string) error {
	rows, err := tx.Query(`SELECT name, origin, partial FROM pragma_index_list(?) WHERE "unique" = 1 ORDER BY seq`, table)
	if err != nil {
		return fmt.Errorf("inspect %s unique constraints: %w", table, err)
	}
	type indexRecord struct {
		name    string
		origin  string
		partial int
	}
	indexes := []indexRecord{}
	for rows.Next() {
		var index indexRecord
		if err := rows.Scan(&index.name, &index.origin, &index.partial); err != nil {
			rows.Close()
			return fmt.Errorf("read %s unique constraint: %w", table, err)
		}
		indexes = append(indexes, index)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate %s unique constraints: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s unique constraints: %w", table, err)
	}
	actualPrimaryKeys := []string{}
	actualUniqueKeys := []string{}
	for _, index := range indexes {
		if index.origin == "pk" {
			columns, err := schemaKeyColumns(tx, index.name)
			if err != nil {
				return err
			}
			actualPrimaryKeys = append(actualPrimaryKeys, schemaKeySignature(columns))
			continue
		}
		if index.origin != "u" {
			return fmt.Errorf("table %q has noncanonical unique index %q with origin %q", table, index.name, index.origin)
		}
		if index.partial != 0 {
			return fmt.Errorf("table %q has unexpected partial unique index %q", table, index.name)
		}
		columns, err := schemaKeyColumns(tx, index.name)
		if err != nil {
			return err
		}
		actualUniqueKeys = append(actualUniqueKeys, schemaKeySignature(columns))
	}

	expectedUniqueKeys := make([]string, 0, len(schemaUniqueKeys[table]))
	for _, key := range schemaUniqueKeys[table] {
		expectedUniqueKeys = append(expectedUniqueKeys, schemaKeySignature(key))
	}
	sort.Strings(actualUniqueKeys)
	sort.Strings(expectedUniqueKeys)
	if !slices.Equal(actualUniqueKeys, expectedUniqueKeys) {
		return fmt.Errorf("table %q has unique keys %v, expected %v", table, actualUniqueKeys, expectedUniqueKeys)
	}

	primaryKeyColumns := schemaPrimaryKeys[table]
	expectedPrimaryKeys := []string{}
	if len(primaryKeyColumns) > 1 || (len(primaryKeyColumns) == 1 && !integerSchemaColumns[table][primaryKeyColumns[0]]) {
		columns := make([]schemaIndexKeyColumn, 0, len(primaryKeyColumns))
		for _, column := range primaryKeyColumns {
			columns = append(columns, schemaIndexKeyColumn{name: column, collation: expectedSchemaCollation(table, column)})
		}
		expectedPrimaryKeys = append(expectedPrimaryKeys, schemaKeySignature(columns))
	}
	sort.Strings(actualPrimaryKeys)
	sort.Strings(expectedPrimaryKeys)
	if !slices.Equal(actualPrimaryKeys, expectedPrimaryKeys) {
		return fmt.Errorf("table %q has primary-key indexes %v, expected %v", table, actualPrimaryKeys, expectedPrimaryKeys)
	}
	return nil
}

func schemaKeySignature(columns []schemaIndexKeyColumn) string {
	parts := make([]string, 0, len(columns))
	for _, column := range columns {
		parts = append(parts, strings.Join([]string{
			strings.ToLower(column.name), strings.ToUpper(column.collation), fmt.Sprint(column.descending),
		}, ":"))
	}
	return strings.Join(parts, ",")
}

func validateTableForeignKeys(tx *sql.Tx, table string) error {
	rows, err := tx.Query(`SELECT "table", "from", "to", upper(on_update), upper(on_delete), upper(match) FROM pragma_foreign_key_list(?)`, table)
	if err != nil {
		return fmt.Errorf("inspect %s foreign keys: %w", table, err)
	}
	actual := []string{}
	for rows.Next() {
		var foreignKey schemaForeignKey
		if err := rows.Scan(&foreignKey.table, &foreignKey.from, &foreignKey.to, &foreignKey.onUpdate, &foreignKey.onDelete, &foreignKey.match); err != nil {
			rows.Close()
			return fmt.Errorf("read %s foreign key: %w", table, err)
		}
		actual = append(actual, schemaForeignKeySignature(foreignKey))
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate %s foreign keys: %w", table, err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close %s foreign keys: %w", table, err)
	}
	expected := make([]string, 0, len(schemaForeignKeys[table]))
	for _, foreignKey := range schemaForeignKeys[table] {
		expected = append(expected, schemaForeignKeySignature(foreignKey))
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if !slices.Equal(actual, expected) {
		return fmt.Errorf("table %q has foreign keys %v, expected %v", table, actual, expected)
	}
	return nil
}

func schemaForeignKeySignature(foreignKey schemaForeignKey) string {
	return strings.Join([]string{
		strings.ToLower(foreignKey.table), strings.ToLower(foreignKey.from), strings.ToLower(foreignKey.to),
		strings.ToUpper(foreignKey.onUpdate), strings.ToUpper(foreignKey.onDelete), strings.ToUpper(foreignKey.match),
	}, ":")
}

func validateCheckConstraints(tx *sql.Tx, table string) error {
	var definition string
	if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&definition); err != nil {
		return fmt.Errorf("read %s table definition: %w", table, err)
	}
	actualExpressions, err := extractCheckExpressions(definition)
	if err != nil {
		return fmt.Errorf("read %s check constraints: %w", table, err)
	}
	actual := map[string]int{}
	for _, expression := range actualExpressions {
		actual[normalizeCheckExpression(expression)]++
	}
	expected := map[string]int{}
	for _, expression := range schemaCheckExpressions[table] {
		normalized := normalizeCheckExpression(expression)
		expected[normalized]++
	}
	if !maps.Equal(actual, expected) {
		return fmt.Errorf("table %q has check constraints %s, expected %s", table, formatCheckExpressionCounts(actual), formatCheckExpressionCounts(expected))
	}
	return nil
}

func validateConstraintConflictPolicies(tx *sql.Tx, table string) error {
	var definition string
	if err := tx.QueryRow(`SELECT sql FROM sqlite_master WHERE type = 'table' AND name = ?`, table).Scan(&definition); err != nil {
		return fmt.Errorf("read %s table definition: %w", table, err)
	}
	for offset := 0; offset < len(definition); {
		next, skipped, err := skipSQLQuotedOrComment(definition, offset)
		if err != nil {
			return fmt.Errorf("read %s constraint conflict policies: %w", table, err)
		}
		if skipped {
			offset = next
			continue
		}
		if !sqlKeywordAt(definition, offset, "ON") {
			offset++
			continue
		}
		conflict, err := skipSQLTrivia(definition, offset+len("ON"))
		if err != nil {
			return fmt.Errorf("read %s constraint conflict policies: %w", table, err)
		}
		if sqlKeywordAt(definition, conflict, "CONFLICT") {
			return fmt.Errorf("table %q has a noncanonical ON CONFLICT constraint policy", table)
		}
		offset += len("ON")
	}
	return nil
}

func formatCheckExpressionCounts(counts map[string]int) string {
	items := make([]string, 0, len(counts))
	for expression, count := range counts {
		items = append(items, fmt.Sprintf("%q: %d", expression, count))
	}
	sort.Strings(items)
	return "[" + strings.Join(items, ", ") + "]"
}

func validateNoUnexpectedViewsAndTriggers(tx *sql.Tx) error {
	rows, err := tx.Query(`SELECT type, name, tbl_name FROM sqlite_master WHERE type IN ('view', 'trigger') AND name NOT GLOB 'sqlite_*' ORDER BY type, name`)
	if err != nil {
		return fmt.Errorf("inspect database views and triggers: %w", err)
	}
	type schemaObject struct {
		kind  string
		name  string
		table string
	}
	objects := []schemaObject{}
	for rows.Next() {
		var object schemaObject
		if err := rows.Scan(&object.kind, &object.name, &object.table); err != nil {
			rows.Close()
			return fmt.Errorf("read database schema object: %w", err)
		}
		objects = append(objects, object)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate database views and triggers: %w", err)
	}
	if err := rows.Close(); err != nil {
		return fmt.Errorf("close database views and triggers: %w", err)
	}
	if len(objects) > 0 {
		object := objects[0]
		return fmt.Errorf("unexpected %s %q attached to %q", object.kind, object.name, object.table)
	}
	return nil
}

func validateCurrentSchemaIndexes(tx *sql.Tx) error {
	expectedIndexes := make(map[string]map[string]bool, len(currentSchemaTables))
	for _, index := range currentSchemaIndexes {
		if expectedIndexes[index.table] == nil {
			expectedIndexes[index.table] = map[string]bool{}
		}
		expectedIndexes[index.table][index.name] = true
	}
	for _, table := range currentSchemaTables {
		rows, err := tx.Query(`SELECT name FROM pragma_index_list(?) WHERE origin = 'c' ORDER BY name`, table.name)
		if err != nil {
			return fmt.Errorf("inspect %s indexes: %w", table.name, err)
		}
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				rows.Close()
				return fmt.Errorf("read %s index: %w", table.name, err)
			}
			if !expectedIndexes[table.name][name] {
				rows.Close()
				return fmt.Errorf("table %q has unexpected index %q", table.name, name)
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return fmt.Errorf("iterate %s indexes: %w", table.name, err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close %s indexes: %w", table.name, err)
		}
	}

	for _, index := range currentSchemaIndexes {
		var unique, partial int
		var origin string
		err := tx.QueryRow(`SELECT "unique", partial, origin FROM pragma_index_list(?) WHERE name = ?`, index.table, index.name).Scan(&unique, &partial, &origin)
		if err == sql.ErrNoRows {
			return fmt.Errorf("required index %q is missing from table %q", index.name, index.table)
		}
		if err != nil {
			return fmt.Errorf("inspect index %q: %w", index.name, err)
		}
		if unique != 0 || partial != 0 || origin != "c" {
			return fmt.Errorf("index %q has unexpected unique=%d partial=%d origin=%q", index.name, unique, partial, origin)
		}
		actual, err := schemaKeyColumns(tx, index.name)
		if err != nil {
			return err
		}
		if len(actual) != len(index.columns) {
			return fmt.Errorf("index %q has %d key columns, expected %d", index.name, len(actual), len(index.columns))
		}
		for position, expected := range index.columns {
			got := actual[position]
			wantCollation := expectedSchemaCollation(index.table, expected.name)
			if got.name != expected.name || got.descending != expected.descending || got.collation != wantCollation {
				return fmt.Errorf("index %q column %d is %q collation=%s descending=%t, expected %q collation=%s descending=%t", index.name, position, got.name, got.collation, got.descending, expected.name, wantCollation, expected.descending)
			}
		}
	}
	return nil
}

func missingSchemaItem(tx *sql.Tx, tables map[string]bool, table schemaTable) (string, error) {
	if !tables[table.name] {
		return fmt.Sprintf("required table %q is missing", table.name), nil
	}
	columns, err := schemaColumnNames(tx, table.name)
	if err != nil {
		return "", err
	}
	for _, column := range table.columns {
		if !columns[column] {
			return fmt.Sprintf("required column %s.%s is missing", table.name, column), nil
		}
	}
	return "", nil
}

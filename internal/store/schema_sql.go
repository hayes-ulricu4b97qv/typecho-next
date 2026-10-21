package store

import (
	"errors"
	"strings"
)

func splitSQLTableDefinitions(definition string) ([]string, error) {
	open := -1
	for position := 0; position < len(definition); {
		next, skipped, err := skipSQLQuotedOrComment(definition, position)
		if err != nil {
			return nil, err
		}
		if skipped {
			position = next
			continue
		}
		if definition[position] == '(' {
			open = position
			break
		}
		position++
	}
	if open < 0 {
		return nil, errors.New("table definition has no opening parenthesis")
	}
	close, err := matchingSQLParenthesis(definition, open)
	if err != nil {
		return nil, err
	}
	body := definition[open+1 : close]
	segments := []string{}
	depth := 0
	start := 0
	for position := 0; position < len(body); {
		next, skipped, err := skipSQLQuotedOrComment(body, position)
		if err != nil {
			return nil, err
		}
		if skipped {
			position = next
			continue
		}
		switch body[position] {
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return nil, errors.New("unbalanced table-definition parenthesis")
			}
		case ',':
			if depth == 0 {
				segments = append(segments, body[start:position])
				start = position + 1
			}
		}
		position++
	}
	if depth != 0 {
		return nil, errors.New("unbalanced table-definition parenthesis")
	}
	segments = append(segments, body[start:])
	return segments, nil
}

func readSQLIdentifier(statement string, offset int) (string, int, bool, error) {
	start, err := skipSQLTrivia(statement, offset)
	if err != nil {
		return "", offset, false, err
	}
	if start >= len(statement) {
		return "", start, false, nil
	}
	opening := statement[start]
	if opening == '"' || opening == '`' || opening == '[' {
		end, skipped, err := skipSQLQuotedOrComment(statement, start)
		if err != nil {
			return "", start, false, err
		}
		if !skipped {
			return "", start, false, nil
		}
		closing := opening
		if opening == '[' {
			closing = ']'
		}
		identifier := strings.ReplaceAll(statement[start+1:end-1], string([]byte{closing, closing}), string(closing))
		return identifier, end, true, nil
	}
	if !isSQLIdentifierByte(opening) {
		return "", start, false, nil
	}
	end := start + 1
	for end < len(statement) && isSQLIdentifierByte(statement[end]) {
		end++
	}
	return statement[start:end], end, true, nil
}

func sqlColumnCollation(definition string) (string, error) {
	collation := "BINARY"
	found := false
	depth := 0
	for position := 0; position < len(definition); {
		next, skipped, err := skipSQLQuotedOrComment(definition, position)
		if err != nil {
			return "", err
		}
		if skipped {
			position = next
			continue
		}
		switch definition[position] {
		case '(':
			depth++
		case ')':
			depth--
		case 'C', 'c':
			if depth == 0 && sqlKeywordAt(definition, position, "COLLATE") {
				if found {
					return "", errors.New("column has multiple COLLATE clauses")
				}
				name, end, ok, err := readSQLIdentifier(definition, position+len("COLLATE"))
				if err != nil {
					return "", err
				}
				if !ok {
					return "", errors.New("COLLATE clause has no collation name")
				}
				collation = strings.ToUpper(name)
				found = true
				position = end
				continue
			}
		}
		position++
	}
	return collation, nil
}

func normalizeCheckExpression(expression string) string {
	var normalized strings.Builder
	pendingSpace := false
	for offset := 0; offset < len(expression); {
		if isSQLWhitespace(expression[offset]) {
			pendingSpace = true
			offset++
			continue
		}

		next, skipped, err := skipSQLQuotedOrComment(expression, offset)
		if err != nil {
			return ""
		}
		if skipped {
			if isSQLCommentStart(expression, offset) {
				pendingSpace = true
				offset = next
				continue
			}
			writePendingSQLSpace(&normalized, &pendingSpace)
			quoted := expression[offset:next]
			if expression[offset] == '\'' {
				normalized.WriteString(quoted)
			} else {
				normalized.WriteString(strings.ToLower(quoted))
			}
			offset = next
			continue
		}

		writePendingSQLSpace(&normalized, &pendingSpace)
		start := offset
		for offset < len(expression) && !isSQLWhitespace(expression[offset]) {
			if _, skipped, _ := skipSQLQuotedOrComment(expression, offset); skipped {
				break
			}
			offset++
		}
		normalized.WriteString(strings.ToLower(expression[start:offset]))
	}
	return normalized.String()
}

func extractCheckExpressions(definition string) ([]string, error) {
	expressions := []string{}
	for offset := 0; offset < len(definition); {
		next, skipped, err := skipSQLQuotedOrComment(definition, offset)
		if err != nil {
			return nil, err
		}
		if skipped {
			offset = next
			continue
		}

		if sqlKeywordAt(definition, offset, "CHECK") {
			open, err := skipSQLTrivia(definition, offset+len("CHECK"))
			if err != nil {
				return nil, err
			}
			if open >= len(definition) || definition[open] != '(' {
				offset += len("CHECK")
				continue
			}
			close, err := matchingSQLParenthesis(definition, open)
			if err != nil {
				return nil, err
			}
			expressions = append(expressions, strings.TrimSpace(definition[open+1:close]))
			offset = close + 1
			continue
		}
		offset++
	}
	return expressions, nil
}

func sqlKeywordAt(statement string, offset int, keyword string) bool {
	end := offset + len(keyword)
	if end > len(statement) || !strings.EqualFold(statement[offset:end], keyword) {
		return false
	}
	if offset > 0 && isSQLIdentifierByte(statement[offset-1]) {
		return false
	}
	return end == len(statement) || !isSQLIdentifierByte(statement[end])
}

func skipSQLTrivia(statement string, offset int) (int, error) {
	for offset < len(statement) {
		if isSQLWhitespace(statement[offset]) {
			offset++
			continue
		}
		if !isSQLCommentStart(statement, offset) {
			return offset, nil
		}
		next, skipped, err := skipSQLQuotedOrComment(statement, offset)
		if err != nil {
			return 0, err
		}
		if !skipped {
			return offset, nil
		}
		offset = next
	}
	return offset, nil
}

func skipSQLQuotedOrComment(statement string, offset int) (int, bool, error) {
	if offset < 0 || offset >= len(statement) {
		return offset, false, nil
	}
	if isSQLCommentStart(statement, offset) {
		if statement[offset] == '-' {
			for offset < len(statement) && statement[offset] != '\n' && statement[offset] != '\r' {
				offset++
			}
			return offset, true, nil
		}
		end := strings.Index(statement[offset+2:], "*/")
		if end < 0 {
			return 0, true, errors.New("unclosed SQL block comment")
		}
		return offset + 2 + end + 2, true, nil
	}

	opening := statement[offset]
	closing := opening
	switch opening {
	case '\'', '"', '`':
	case '[':
		closing = ']'
	default:
		return offset, false, nil
	}
	for cursor := offset + 1; cursor < len(statement); cursor++ {
		if statement[cursor] != closing {
			continue
		}
		if cursor+1 < len(statement) && statement[cursor+1] == closing {
			cursor++
			continue
		}
		return cursor + 1, true, nil
	}
	return 0, true, errors.New("unclosed SQL quoted token")
}

func isSQLCommentStart(statement string, offset int) bool {
	return offset+1 < len(statement) &&
		(statement[offset] == '-' && statement[offset+1] == '-' || statement[offset] == '/' && statement[offset+1] == '*')
}

func isSQLWhitespace(value byte) bool {
	return value == ' ' || value == '\t' || value == '\r' || value == '\n' || value == '\f'
}

func writePendingSQLSpace(builder *strings.Builder, pending *bool) {
	if *pending && builder.Len() > 0 {
		builder.WriteByte(' ')
	}
	*pending = false
}

func isSQLIdentifierByte(value byte) bool {
	return value >= 0x80 || value == '_' || value >= 'a' && value <= 'z' || value >= 'A' && value <= 'Z' || value >= '0' && value <= '9'
}

func matchingSQLParenthesis(statement string, open int) (int, error) {
	depth := 0
	for position := open; position < len(statement); {
		next, skipped, err := skipSQLQuotedOrComment(statement, position)
		if err != nil {
			return 0, err
		}
		if skipped {
			position = next
			continue
		}
		value := statement[position]
		switch value {
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return position, nil
			}
		}
		position++
	}
	return 0, errors.New("unclosed parenthesis in table definition")
}

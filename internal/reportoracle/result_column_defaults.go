package reportoracle

import "strings"

// Keep default expressions internal. Exclude explicit NULL and Oracle's empty
// string literal without evaluating arbitrary SQL expressions from metadata.
func hasNonNullColumnDefault(expression string) bool {
	expression = strings.TrimSpace(expression)
	for strings.HasPrefix(expression, "(") && strings.HasSuffix(expression, ")") {
		expression = strings.TrimSpace(expression[1 : len(expression)-1])
	}
	return expression != "" && expression != "''" && !strings.EqualFold(expression, "NULL")
}

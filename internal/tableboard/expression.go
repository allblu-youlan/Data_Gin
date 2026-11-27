package tableboard

import (
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// Expressions refer only to cells in the same section. Tokenize the supported
// spreadsheet syntax directly: no script evaluation or SQL string expansion.
type Position struct{ Row, Column int }
type expressionNode struct {
	op          string
	text        string
	first, last Position
	children    []*expressionNode
}

var expressionToken = regexp.MustCompile(`^(?:\$?[A-Z]+\$?[1-9][0-9]*|[A-Z]+|(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)|""|"-"|[-+*/():,])`)
var cellReference = regexp.MustCompile(`^\$?([A-Z]+)\$?([1-9][0-9]*)$`)
var decimalLiteral = regexp.MustCompile(`^[+-]?(?:[0-9]+(?:\.[0-9]*)?|\.[0-9]+)(?:[eE][+-]?[0-9]{1,3})?$`)

type expressionParser struct {
	rest, token string
	rows, cols  int
}

func (p *expressionParser) next() {
	p.rest = strings.TrimSpace(p.rest)
	if p.rest == "" {
		p.token = ""
		return
	}
	p.token = expressionToken.FindString(p.rest)
	if p.token == "" {
		p.token = "?"
		return
	}
	p.rest = p.rest[len(p.token):]
}
func (p *expressionParser) take(s string) bool {
	if p.token != s {
		return false
	}
	p.next()
	return true
}
func (p *expressionParser) position() (Position, error) {
	match := cellReference.FindStringSubmatch(p.token)
	if match == nil || len(match[1]) > 2 {
		return Position{}, errors.New("无效的格地址")
	}
	col := 0
	for _, c := range match[1] {
		col = col*26 + int(c-'A'+1)
	}
	row, e := strconv.Atoi(match[2])
	if e != nil || row > p.rows || col > p.cols {
		return Position{}, fmt.Errorf("引用 %s 超出当前区块", p.token)
	}
	p.next()
	return Position{row - 1, col - 1}, nil
}
func (p *expressionParser) parse(minimum int) (*expressionNode, error) {
	left, e := p.atom()
	if e != nil {
		return nil, e
	}
	for {
		op := p.token
		prec := 0
		switch op {
		case "+", "-":
			prec = 1
		case "*", "/":
			prec = 2
		}
		if prec < minimum {
			break
		}
		p.next()
		right, e := p.parse(prec + 1)
		if e != nil {
			return nil, e
		}
		left = &expressionNode{op: op, children: []*expressionNode{left, right}}
	}
	return left, nil
}
func (p *expressionParser) atom() (*expressionNode, error) {
	token := p.token
	switch token {
	case "+", "-":
		p.next()
		arg, e := p.atom()
		return &expressionNode{op: "unary" + token, children: []*expressionNode{arg}}, e
	case "(":
		p.next()
		n, e := p.parse(1)
		if e != nil {
			return nil, e
		}
		if !p.take(")") {
			return nil, errors.New("缺少右括号")
		}
		return n, nil
	case "SUM", "IFERROR":
		p.next()
		if !p.take("(") {
			return nil, errors.New("函数缺少左括号")
		}
		n := &expressionNode{op: token}
		for {
			arg, e := p.parse(1)
			if e != nil {
				return nil, e
			}
			if token == "SUM" && p.take(":") {
				if arg.op != "ref" {
					return nil, errors.New("范围必须以单元格开始")
				}
				arg.op = "range"
				arg.last, e = p.position()
				if e != nil {
					return nil, e
				}
				if arg.last.Row < arg.first.Row || arg.last.Column < arg.first.Column {
					return nil, errors.New("范围终点必须在起点右下方")
				}
			}
			n.children = append(n.children, arg)
			if token == "IFERROR" {
				if !p.take(",") || (p.token != `""` && p.token != `"-"`) {
					return nil, errors.New("IFERROR 的备用值须为空文本或减号")
				}
				n.text = strings.Trim(p.token, `"`)
				p.next()
				break
			}
			if !p.take(",") {
				break
			}
		}
		if !p.take(")") {
			return nil, errors.New("函数参数或右括号无效")
		}
		return n, nil
	}
	if cellReference.MatchString(token) {
		pos, e := p.position()
		return &expressionNode{op: "ref", first: pos, last: pos}, e
	}
	if len(token) <= 100 && decimalLiteral.MatchString(token) {
		p.next()
		return &expressionNode{op: "number", text: token}, nil
	}
	return nil, errors.New("仅支持数字、本区块格地址、四则运算、SUM 和 IFERROR")
}
func (n *expressionNode) references(visit func(Position) error) error {
	if n.op == "ref" || n.op == "range" {
		for r := n.first.Row; r <= n.last.Row; r++ {
			for c := n.first.Column; c <= n.last.Column; c++ {
				if e := visit(Position{r, c}); e != nil {
					return e
				}
			}
		}
	}
	for _, child := range n.children {
		if e := child.references(visit); e != nil {
			return e
		}
	}
	return nil
}

type Expressions struct {
	nodes map[Position]*expressionNode
	order []Position
}

func CompileExpressions(section Section) (Expressions, error) {
	plan := Expressions{nodes: map[Position]*expressionNode{}}
	for r, row := range section.Cells {
		for col, cell := range row {
			if cell.Kind != "expression" {
				continue
			}
			if len(cell.Expression) > 2048 {
				return plan, fmt.Errorf("%s 的表达式超过 2048 字节", Address(r, col))
			}
			p := expressionParser{rows: len(section.Cells), cols: len(row), rest: strings.ToUpper(strings.TrimPrefix(strings.TrimSpace(cell.Expression), "="))}
			p.next()
			n, e := p.parse(1)
			if e != nil {
				return plan, fmt.Errorf("%s：%w", Address(r, col), e)
			}
			if p.token != "" {
				return plan, fmt.Errorf("%s 的表达式有多余字符", Address(r, col))
			}
			plan.nodes[Position{r, col}] = n
		}
	}
	state := map[Position]int{}
	var visit func(Position) error
	visit = func(pos Position) error {
		n := plan.nodes[pos]
		if n == nil || state[pos] == 2 {
			return nil
		}
		if state[pos] == 1 {
			return fmt.Errorf("%s 存在循环引用", Address(pos.Row, pos.Column))
		}
		state[pos] = 1
		if e := n.references(visit); e != nil {
			return e
		}
		state[pos] = 2
		plan.order = append(plan.order, pos)
		return nil
	}
	for pos := range plan.nodes {
		if e := visit(pos); e != nil {
			return plan, e
		}
	}
	return plan, nil
}

// ExpressionValue keeps unavailable data separate from arithmetic errors so
// IFERROR cannot conceal an inaccessible source or a failed upstream query.
type ExpressionValue struct{ Status, Value, Message string }
type operand struct {
	num         *big.Rat
	text        string
	err         error
	unavailable ExpressionValue
}

func readOperand(value ExpressionValue) operand {
	if value.Status != "ok" {
		return operand{unavailable: value}
	}
	text := strings.TrimSpace(value.Value)
	if len(text) <= 310 && decimalLiteral.MatchString(text) {
		if n, ok := new(big.Rat).SetString(text); ok {
			return operand{num: n}
		}
	}
	return operand{text: value.Value}
}
func (o operand) number() (*big.Rat, error) {
	if o.num != nil {
		return o.num, nil
	}
	if o.text == "" {
		return new(big.Rat), nil
	}
	return nil, errors.New("参与运算的单元格不是数值")
}
func bounded(n *big.Rat) operand {
	if n.Num().BitLen() > 4096 || n.Denom().BitLen() > 4096 {
		return operand{err: errors.New("表达式数值超出精度范围")}
	}
	return operand{num: n}
}
func (n *expressionNode) eval(get func(Position) operand) operand {
	switch n.op {
	case "number":
		return readOperand(ExpressionValue{Status: "ok", Value: n.text})
	case "ref":
		return get(n.first)
	case "SUM":
		total := new(big.Rat)
		add := func(v operand) error {
			if v.err != nil {
				return v.err
			}
			if v.num != nil {
				total.Add(total, v.num)
				return bounded(total).err
			}
			return nil
		}
		for _, arg := range n.children {
			if arg.op == "range" {
				if e := arg.references(func(p Position) error { return add(get(p)) }); e != nil {
					return operand{err: e}
				}
			} else if e := add(arg.eval(get)); e != nil {
				return operand{err: e}
			}
		}
		return bounded(total)
	}
	a := n.children[0].eval(get)
	if n.op == "IFERROR" {
		if a.err != nil {
			return operand{text: n.text}
		}
		return a
	}
	if a.err != nil {
		return a
	}
	x, e := a.number()
	if e != nil {
		return operand{err: e}
	}
	switch n.op {
	case "unary+":
		return operand{num: x}
	case "unary-":
		return operand{num: new(big.Rat).Neg(x)}
	}
	b := n.children[1].eval(get)
	if b.err != nil {
		return b
	}
	y, e := b.number()
	if e != nil {
		return operand{err: e}
	}
	z := new(big.Rat)
	switch n.op {
	case "+":
		z.Add(x, y)
	case "-":
		z.Sub(x, y)
	case "*":
		z.Mul(x, y)
	case "/":
		if y.Sign() == 0 {
			return operand{err: errors.New("除数为零")}
		}
		z.Quo(x, y)
	}
	return bounded(z)
}
func (o operand) value() ExpressionValue {
	if o.unavailable.Status != "" {
		return o.unavailable
	}
	if o.err != nil {
		return ExpressionValue{Status: "error", Message: o.err.Error()}
	}
	text := o.text
	if o.num != nil {
		precision, exact := o.num.FloatPrec()
		if !exact {
			precision = 34
		}
		text = o.num.FloatString(precision)
		if strings.Contains(text, ".") {
			text = strings.TrimSuffix(strings.TrimRight(text, "0"), ".")
		}
	}
	return ExpressionValue{Status: "ok", Value: text}
}

func (plan Expressions) Evaluate(read func(Position) ExpressionValue) map[Position]ExpressionValue {
	cache := map[Position]operand{}
	get := func(pos Position) operand {
		if v, ok := cache[pos]; ok {
			return v
		}
		return readOperand(read(pos))
	}
	result := map[Position]ExpressionValue{}
	for _, pos := range plan.order {
		n := plan.nodes[pos]
		var unavailable ExpressionValue
		// Inspect every dependency before arithmetic, including the IFERROR operand.
		_ = n.references(func(p Position) error {
			v := get(p).unavailable
			if v.Status == "error" || unavailable.Status == "" {
				unavailable = v
			}
			return nil
		})
		v := operand{unavailable: unavailable}
		if unavailable.Status == "" {
			v = n.eval(get)
		}
		result[pos] = v.value()
		if v.err != nil {
			v = operand{unavailable: result[pos]}
		}
		cache[pos] = v
	}
	return result
}

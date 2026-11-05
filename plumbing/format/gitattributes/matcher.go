package gitattributes

// Matcher defines a global multi-pattern matcher for gitattributes patterns
type Matcher interface {
	// Match matches patterns in the order of priorities.
	Match(path, attributes []string) (map[string]Attribute, bool)
}

// MatcherOptions contains options for the Matcher.
type MatcherOptions struct{}

// builtinMacros are the macros Git defines before any attributes file, so
// that a file may redefine them.
var builtinMacros = []string{"[attr]binary -diff -merge -text"}

// NewMatcher constructs a new matcher. Patterns must be given in the order of
// increasing priority. That is the most generic settings files first, then the
// content of the repo .gitattributes, then content of .gitattributes down the
// path.
func NewMatcher(stack []MatchAttribute) Matcher {
	m := &matcher{stack: stack}
	m.init()

	return m
}

type matcher struct {
	stack  []MatchAttribute
	macros map[string]MatchAttribute
}

func (m *matcher) init() {
	m.macros = make(map[string]MatchAttribute)

	for _, line := range builtinMacros {
		macro, err := ParseAttributesLine(line, nil, true)
		if err != nil {
			panic(err)
		}
		m.macros[macro.Name] = macro
	}

	// A later definition has a higher priority, as for patterns.
	for _, attr := range m.stack {
		if attr.Pattern == nil {
			m.macros[attr.Name] = attr
		}
	}
}

// Match matches path against the patterns in gitattributes files and returns
// the attributes associated with the path.
//
// Specific attributes can be specified otherwise all attributes are returned.
//
// As in Git, each attribute takes its state from the matching line of the
// highest priority that names it: the last line of the most specific file,
// and, within a line, the last mention. An attribute set to true that is a
// macro sets the attributes of the macro in turn, again only those that are
// still undetermined.
//
// Matched is true if any path was matched to a rule, even if the results map
// is empty.
//
// See https://github.com/git/git/blob/v2.54.0/attr.c#L1092-L1136.
func (m *matcher) Match(path, attributes []string) (results map[string]Attribute, matched bool) {
	requested := make(map[string]bool, len(attributes))
	for _, a := range attributes {
		requested[a] = true
	}
	unresolved := len(requested)
	done := func() bool { return len(requested) > 0 && unresolved == 0 }

	determined := make(map[string]Attribute)
	for i := len(m.stack) - 1; i >= 0 && !done(); i-- {
		pattern := m.stack[i].Pattern
		if pattern == nil || !pattern.Match(path) {
			continue
		}
		matched = true

		// The queue is consumed from its end: the last mention on a line
		// first, and a macro's attributes before the rest of its line.
		queue := append([]Attribute(nil), m.stack[i].Attributes...)
		for len(queue) > 0 && !done() {
			attr := queue[len(queue)-1]
			queue = queue[:len(queue)-1]

			name := attr.Name()
			if _, ok := determined[name]; ok {
				continue
			}
			determined[name] = attr
			if requested[name] {
				unresolved--
			}
			if macro, ok := m.macros[name]; ok && attr.IsSet() {
				queue = append(queue, macro.Attributes...)
			}
		}
	}

	if len(requested) == 0 {
		return determined, matched
	}
	results = make(map[string]Attribute, len(requested))
	for name := range requested {
		if attr, ok := determined[name]; ok {
			results[name] = attr
		}
	}
	return results, matched
}

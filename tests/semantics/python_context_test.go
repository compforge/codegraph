package semantics_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/compforge/codegraph"
)

// +spec=`Parameter targets bind only local names; defaults remain in the prelude`
func TestPythonContextParameterBindings(t *testing.T) {
	g, err := codegraph.NewBuilder("parameters", codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(context.Background(), codegraph.Document{Path: "api.py", Content: []byte(
		"def request(value: Annotated[str, Field(gt=0)] = make_default(), *args: str, **kwargs: int):\n    return value\n")})
	if err != nil {
		t.Fatal(err)
	}
	fn := facts.Statements[0]
	var names []string
	var visit func(codegraph.Expression)
	visit = func(e codegraph.Expression) {
		if e.Kind == "identifier" {
			names = append(names, e.Text)
		}
		for _, child := range e.Children {
			visit(child)
		}
	}
	visit(fn.Target)
	if !slices.Equal(names, []string{"value", "args", "kwargs"}) {
		t.Fatalf("parameter bindings: %v", names)
	}
	if len(fn.Prelude) != 1 || fn.Prelude[0].Kind != "call" || fn.Prelude[0].Children[0].Text != "make_default" {
		t.Fatalf("default evaluation: %+v", fn.Prelude)
	}
}

// +case=`Generated API annotations and docstrings leave budget for later imports`
func TestPythonGeneratedAPIContext(t *testing.T) {
	var source strings.Builder
	source.WriteString("class Api:\n")
	for i := 0; i < 160; i++ {
		fmt.Fprintf(&source, `    # Generated endpoint documentation.
    def endpoint%d(self, timeout: Union[None, Annotated[float, Field(gt=0)], Tuple[Annotated[float, Field(gt=0)], Annotated[float, Field(gt=0)]]] = None, headers: Optional[Dict[str, Any]] = None):
        """Endpoint documentation.
        This describes parameters and return values.
        """
        return timeout
`, i)
	}
	source.WriteString("import final_dependency\n")
	g, err := codegraph.NewBuilder("generated", codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(context.Background(), codegraph.Document{Path: "api.py", Content: []byte(source.String())})
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range facts.Issues {
		if issue.Code == "context_limit" {
			t.Fatalf("generated context truncated: %+v", issue)
		}
	}
	if len(facts.Statements) != 2 || len(facts.Statements[0].Body) != 160 || len(facts.Statements[1].Imports) != 1 || facts.Statements[1].Imports[0].Path != "final_dependency" {
		t.Fatalf("generated statements or final import lost: %d statements", len(facts.Statements))
	}
}

func TestPythonContextStillReportsExhaustion(t *testing.T) {
	g, err := codegraph.NewBuilder("budget", codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(context.Background(), codegraph.Document{Path: "large.py", Content: []byte(strings.Repeat("value = 1\n", 4000))})
	if err != nil {
		t.Fatal(err)
	}
	for _, issue := range facts.Issues {
		if issue.Code == "context_limit" && issue.Subject == codegraph.ContextSubject {
			return
		}
	}
	t.Fatal("real extraction exhaustion was not reported")
}

func TestPythonStringInterpolationRetainsCalls(t *testing.T) {
	g, err := codegraph.NewBuilder("strings", codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(context.Background(), codegraph.Document{Path: "strings.py", Content: []byte(
		"literal = \"\"\"multiline\nplain text\"\"\"\ninterpolated = f'{import_module(\"plugin\")}'\n")})
	if err != nil {
		t.Fatal(err)
	}
	literal := facts.Statements[0].Value
	if literal.Kind != "unknown" || len(literal.Children) != 0 {
		t.Fatalf("literal text expanded: %+v", literal)
	}
	var calls int
	var visit func(codegraph.Expression)
	visit = func(e codegraph.Expression) {
		if e.Kind == "call" && len(e.Children) > 0 && e.Children[0].Text == "import_module" {
			calls++
		}
		for _, child := range e.Children {
			visit(child)
		}
	}
	visit(facts.Statements[1].Value)
	if calls != 1 {
		t.Fatalf("interpolation calls: %d", calls)
	}
}

func TestPythonContextLambdaDefaultRetainsCalls(t *testing.T) {
	g, err := codegraph.NewBuilder("lambda", codegraph.Options{})
	if err != nil {
		t.Fatal(err)
	}
	facts, err := g.Extract(context.Background(), codegraph.Document{Path: "lambda.py", Content: []byte(
		"load = lambda value=import_module(\"plugin\"): value\n")})
	if err != nil {
		t.Fatal(err)
	}
	value := facts.Statements[0].Value
	if value.Kind != "lambda" || len(value.Children) != 2 {
		t.Fatalf("lambda expression: %+v", value)
	}
	parameter := value.Children[0].Children[0]
	if parameter.Kind != "default_parameter" || len(parameter.Children) != 2 || parameter.Children[1].Kind != "call" {
		t.Fatalf("lambda default lost: %+v", parameter)
	}
}

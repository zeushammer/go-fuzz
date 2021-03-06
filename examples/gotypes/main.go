package gotypes

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/format"
	"go/parser"
	"go/token"
	"io/ioutil"
	"os"
	"os/exec"
	"regexp"
	"strings"

	_ "golang.org/x/tools/go/gcimporter"
	"golang.org/x/tools/go/ssa"
	"golang.org/x/tools/go/types"
)

// https://github.com/golang/go/issues/11327
var bigNum = regexp.MustCompile("(\\.[0-9]*)|([0-9]+)[eE]\\-?\\+?[0-9]{3,}")

// https://github.com/golang/go/issues/11274
var formatBug1 = regexp.MustCompile("\\*/[ \t\n\r\f\v]*;")
var formatBug2 = regexp.MustCompile(";[ \t\n\r\f\v]*/\\*")

var issue11528 = regexp.MustCompile("/\\*(.*\n)+.*\\*/")
var issue11533 = regexp.MustCompile("[ \r\t\n=\\+\\-\\*\\^\\/\\(,]0[0-9]+[ieE]")
var issue11531 = regexp.MustCompile(",[ \t\r\n]*,")

var fpRounding = regexp.MustCompile(" \\(untyped float constant .*\\) truncated to ")

var gcCrash = regexp.MustCompile("\n/tmp/fuzz\\.gc[0-9]+:[0-9]+: internal compiler error: ")
var gccgoCrash = regexp.MustCompile("\ngo1: internal compiler error:")
var asanCrash = regexp.MustCompile("\n==[0-9]+==ERROR: AddressSanitizer: ")

func Fuzz(data []byte) int {
	if bigNum.Match(data) {
		return 0
	}
	if issue11531.Match(data) {
		// gccgo hangs on this.
		// https://github.com/golang/go/issues/11531
		return 0
	}
	goErr := gotypes(data)
	//gcErr := gc(data)
	gcErr := goErr
	gccgoErr := gccgo(data)
	if goErr == nil && gcErr != nil && strings.Contains(gcErr.Error(), "line number out of range") {
		// https://github.com/golang/go/issues/11329
		return 0
	}
	if goErr == nil && gcErr != nil && strings.Contains(gcErr.Error(), "stupid shift:") {
		// https://github.com/golang/go/issues/11328
		return 0
	}
	if gcErr == nil && goErr != nil && strings.Contains(goErr.Error(), "untyped float constant") {
		// https://github.com/golang/go/issues/11350
		return 0
	}
	if goErr == nil && gcErr != nil && strings.Contains(gcErr.Error(), "overflow in int -> string") {
		// https://github.com/golang/go/issues/11330
		return 0
	}
	if gcErr == nil && goErr != nil && strings.Contains(goErr.Error(), "illegal character U+") {
		// https://github.com/golang/go/issues/11359
		return 0
	}
	if goErr == nil && gcErr != nil && strings.Contains(gcErr.Error(), "larger than address space") {
		// Gc is more picky at rejecting huge objects.
		return 0
	}
	if goErr == nil && gcErr != nil && strings.Contains(gcErr.Error(), "non-canonical import path") {
		return 0
	}

	if gccgoErr == nil && goErr != nil {
		if strings.Contains(goErr.Error(), "invalid operation: stupid shift count") {
			// https://github.com/golang/go/issues/11524
			return 0
		}
		if (bytes.Contains(data, []byte("//line")) || bytes.Contains(data, []byte("/*"))) &&
			(strings.Contains(goErr.Error(), "illegal UTF-8 encoding") ||
				strings.Contains(goErr.Error(), "illegal character NUL")) {
			// https://github.com/golang/go/issues/11527
			return 0
		}
		if strings.Contains(goErr.Error(), "invalid operation: operator ^ not defined for") {
			// https://github.com/golang/go/issues/11529
			return 0
		}
		if fpRounding.MatchString(goErr.Error()) {
			// gccgo has different rounding
			return 0
		}
		if bytes.Contains(data, []byte("_")) &&
			(strings.Contains(goErr.Error(), ": undeclared name: ") || strings.Contains(goErr.Error(), "invalid array length")) {
			// https://github.com/golang/go/issues/11547
			// https://github.com/golang/go/issues/11535
			return 0
		}
		if strings.Contains(goErr.Error(), "not enough arguments for complex") {
			// https://github.com/golang/go/issues/11561
			return 0
		}
		if strings.Contains(goErr.Error(), "operator | not defined for") {
			// https://github.com/golang/go/issues/11566
			return 0
		}
		if strings.Contains(goErr.Error(), "nil (untyped nil value) is not a type") {
			// https://github.com/golang/go/issues/11567
			return 0
		}
		if strings.Contains(goErr.Error(), "(built-in) must be called") {
			// https://github.com/golang/go/issues/11570
			return 0
		}
		if strings.Contains(goErr.Error(), "redeclared in this block") {
			// https://github.com/golang/go/issues/11573
			return 0
		}
		if strings.Contains(goErr.Error(), "illegal byte order mark") {
			// on "package\rG\n//line \ufeff:1" input, not filed.
			return 0
		}
		if strings.Contains(goErr.Error(), "unknown escape sequence") {
			// https://github.com/golang/go/issues/11575
			return 0
		}
	}

	if goErr == nil && gccgoErr != nil {
		if strings.Contains(gccgoErr.Error(), "error: string index out of bounds") {
			// https://github.com/golang/go/issues/11522
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "error: integer constant overflow") {
			// https://github.com/golang/go/issues/11525
			return 0
		}
		if issue11533.Match(data) {
			// https://github.com/golang/go/issues/11532
			// https://github.com/golang/go/issues/11533
			return 0
		}
		if bytes.Contains(data, []byte("0i")) &&
			(strings.Contains(gccgoErr.Error(), "incompatible types in binary expression") ||
				strings.Contains(gccgoErr.Error(), "initialization expression has wrong type")) {
			// https://github.com/golang/go/issues/11564
			// https://github.com/golang/go/issues/11563
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "invalid character 0x37f in input file") {
			// https://github.com/golang/go/issues/11569
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "error: incompatible types in binary expression") {
			// https://github.com/golang/go/issues/11572
			return 0
		}
	}

	if goErr == nil && gccgoErr != nil && strings.Contains(gccgoErr.Error(), ": error: import file ") {
		// Temporal workaround for broken gccgo installation.
		// Remove this.
		return 0
	}

	if (goErr == nil && gccgoErr != nil || goErr != nil && gccgoErr == nil) && issue11528.Match(data) {
		// https://github.com/golang/go/issues/11528
		return 0
	}

	// go-fuzz is too smart so it can generate a program that contains "internal compiler error" in an error message :)
	if gcErr != nil && gcCrash.MatchString(gcErr.Error()) {
		if strings.Contains(gcErr.Error(), "internal compiler error: out of fixed registers") {
			// https://github.com/golang/go/issues/11352
			return 0
		}
		if strings.Contains(gcErr.Error(), "internal compiler error: naddr: bad HMUL") {
			// https://github.com/golang/go/issues/11358
			return 0
		}
		if strings.Contains(gcErr.Error(), "internal compiler error: treecopy Name") {
			// https://github.com/golang/go/issues/11361
			return 0
		}
		fmt.Printf("gc result: %v\n", gcErr)
		panic("gc compiler crashed")
	}

	if gccgoErr != nil && gccgoCrash.MatchString(gccgoErr.Error()) {
		if strings.Contains(gccgoErr.Error(), "warning: no arguments for builtin function ‘print’") {
			// https://github.com/golang/go/issues/11526
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "error: constant refers to itself") {
			// https://github.com/golang/go/issues/11536
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in set_type, at go/gofrontend/expressions.cc") {
			// https://github.com/golang/go/issues/11537
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in global_variable_set_init, at go/go-gcc.cc") {
			// https://github.com/golang/go/issues/11541
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in wide_int_to_tree, at tree.c") {
			// https://github.com/golang/go/issues/11542
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in record_var_depends_on, at go/gofrontend/gogo.h") {
			// https://github.com/golang/go/issues/11543
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in Builtin_call_expression, at go/gofrontend/expressions.cc") {
			// https://github.com/golang/go/issues/11544
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in check_bounds, at go/gofrontend/expressions.cc") {
			// https://github.com/golang/go/issues/11545
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in do_determine_type, at go/gofrontend/expressions.h") {
			// https://github.com/golang/go/issues/11546
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in backend_numeric_constant_expression, at go/gofrontend/expressions.cc") {
			// https://github.com/golang/go/issues/11548
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in declare_function, at go/gofrontend/gogo.cc") {
			// https://github.com/golang/go/issues/11557
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "gcc/go/gofrontend/expressions.cc:5756") {
			// https://github.com/golang/go/issues/11558
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "Send_statement::do_flatten") {
			// https://github.com/golang/go/issues/11559
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "internal compiler error: in do_get_backend, at go/gofrontend/expressions.cc") {
			// https://github.com/golang/go/issues/11560
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in type_size, at go/go-gcc.cc") {
			// https://github.com/golang/go/issues/11554
			// https://github.com/golang/go/issues/11555
			// https://github.com/golang/go/issues/11556
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in create_tmp_var, at gimple-expr.c") {
			// https://github.com/golang/go/issues/11568
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in start_function, at go/gofrontend/gogo.cc") {
			// https://github.com/golang/go/issues/11576
			return 0
		}
		if strings.Contains(gccgoErr.Error(), "go1: internal compiler error: in methods, at go/gofrontend/types.cc") {
			// https://github.com/golang/go/issues/11579
			return 0
		}
		fmt.Printf("gccgo result: %v\n", gccgoErr)
		panic("gccgo compiler crashed")
	}

	if gccgoErr != nil && asanCrash.MatchString(gccgoErr.Error()) {
		if strings.Contains(gccgoErr.Error(), " in Lex::skip_cpp_comment() ../../gcc/go/gofrontend/lex.cc") {
			// https://github.com/golang/go/issues/11577
			return 0
		}
		fmt.Printf("gccgo result: %v\n", gccgoErr)
		panic("gccgo compiler crashed")
	}

	if (goErr == nil) != (gcErr == nil) || (goErr == nil) != (gccgoErr == nil) {
		fmt.Printf("go/types result: %v\n", goErr)
		fmt.Printf("gc result: %v\n", gcErr)
		fmt.Printf("gccgo result: %v\n", gccgoErr)
		panic("gc, gccgo and go/types disagree")
	}
	if goErr != nil {
		return 0

	}
	if formatBug1.Match(data) || formatBug2.Match(data) {
		return 1
	}
	// https://github.com/golang/go/issues/11274
	data = bytes.Replace(data, []byte{'\r'}, []byte{' '}, -1)
	data1, err := format.Source(data)
	if err != nil {
		panic(err)
	}
	if false {
		err = gotypes(data1)
		if err != nil {
			fmt.Printf("new: %q\n", data1)
			fmt.Printf("err: %v\n", err)
			panic("program become invalid after gofmt")
		}
	}
	return 1
}

func gotypes(data []byte) (err error) {
	fset := token.NewFileSet()
	var f *ast.File
	f, err = parser.ParseFile(fset, "src.go", data, parser.ParseComments|parser.DeclarationErrors|parser.AllErrors)
	if err != nil {
		return
	}
	// provide error handler
	// initialize maps in config
	conf := &types.Config{
		Error: func(err error) {},
		Sizes: &types.StdSizes{4, 8},
	}
	_, err = conf.Check("pkg", fset, []*ast.File{f}, nil)
	if err != nil {
		return
	}
	prog := ssa.NewProgram(fset, ssa.BuildSerially|ssa.SanityCheckFunctions|ssa.GlobalDebug)
	prog.BuildAll()
	for _, pkg := range prog.AllPackages() {
		_, err := pkg.WriteTo(ioutil.Discard)
		if err != nil {
			panic(err)
		}
	}
	return
}

func gc(data []byte) error {
	f, err := ioutil.TempFile("", "fuzz.gc")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	_, err = f.Write(data)
	if err != nil {
		return err
	}
	f.Close()
	out, err := exec.Command("compile", f.Name()).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s\n%s", out, err)
	}
	return nil
}

func gccgo(data []byte) error {
	//cmd := exec.Command("gccgo", "-c", "-x", "go", "-o", "/dev/null", "-")
	cmd := exec.Command("go1", "-", "-o", "/dev/null", "-quiet", "-mtune=generic", "-march=x86-64", "-O3")
	cmd.Stdin = bytes.NewReader(data)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s\n%s", out, err)
	}
	return nil
}

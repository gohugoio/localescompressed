package main

import (
	"bytes"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/token"
	"io"
	"io/fs"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"golang.org/x/tools/go/packages"
)

const header = `
// This file is autogenerated.
package localescompressed
`

func main() {
	createTranslatorsMap()
	createCurrenciesMap()
}

func createTranslatorsMap() {
	const localeMod = "github.com/gohugoio/locales"
	b := &bytes.Buffer{}
	cmd := exec.Command("go", "list", "-m", "-json", localeMod)
	cmd.Stdout = b

	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}

	m := make(map[string]interface{})
	if err := json.Unmarshal(b.Bytes(), &m); err != nil {
		log.Fatal(err)
	}

	dir := m["Dir"].(string)

	var packageNames []string

	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if !d.IsDir() || d.Name() == "currency" {
			return nil
		}

		name := filepath.Base(path)

		if _, err := os.Stat(filepath.Join(path, fmt.Sprintf("%s.go", name))); err == nil {
			packageNames = append(packageNames, name)
		}

		return nil
	})

	sort.Strings(packageNames)

	cfg := &packages.Config{Mode: packages.NeedSyntax | packages.NeedFiles}
	f, err := os.Create(filepath.Join("..", "locales.autogen.go"))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	fmt.Fprint(f, header)
	fmt.Fprintf(f, "import(\n\"math\"\n\"time\"\n\"strconv\"\n\"github.com/gohugoio/locales\"\n\"github.com/gohugoio/locales/currency\"\n)\n\n")

	var (
		allFields []string
		methodSet = make(map[string]bool)
		initW     = &bytes.Buffer{}
		counter   = 0
	)

	for _, k := range packageNames {
		counter++

		if counter%50 == 0 {
			fmt.Printf("[%d] Handling locale %s ...\n", counter, k)
		}

		pkgs, err := packages.Load(cfg, fmt.Sprintf("github.com/gohugoio/locales/%s", k))
		if err != nil {
			log.Fatal(err)
		}

		pkg := pkgs[0]
		gf := pkg.GoFiles[0]

		src, err := ioutil.ReadFile(gf)
		if err != nil {
			log.Fatal(err)
		}

		collector := &coll{src: src, locale: k, w: f, initW: initW, methodSet: methodSet}

		for _, f := range pkg.Syntax {
			for _, node := range f.Decls {
				ast.Inspect(node, collector.collectFields)
			}
		}

		collector.fields = uniqueStringsSorted(collector.fields)
		collector.methods = uniqueStringsSorted(collector.methods)
		allFields = append(allFields, collector.fields...)
		allFields = uniqueStringsSorted(allFields)

		for _, f := range pkg.Syntax {
			for _, node := range f.Decls {
				ast.Inspect(node, collector.collectNew)
			}
		}

	}

	fmt.Fprint(f, "\ntype localen struct {\n")
	for _, field := range allFields {
		fmt.Fprintf(f, "\t%s\n", field)
	}

	fmt.Fprint(f, "\n}\n")

	fmt.Fprint(f, "func init() {\n")
	fmt.Fprint(f, initW.String())
	fmt.Fprint(f, "\n}")
}

type coll struct {
	// Global
	w         io.Writer
	initW     io.Writer
	methodSet map[string]bool

	// Local
	locale  string
	fields  []string
	methods []string
	src     []byte
}

func (c *coll) collectFields(node ast.Node) bool {
	switch vv := node.(type) {

	case *ast.FuncDecl:
		if vv.Recv != nil {
			recName := vv.Recv.List[0].Names[0].Name
			name := vv.Name.Name

			start := vv.Pos() - 1
			end := vv.End()

			body := string(c.src[start : end-1])
			re := regexp.MustCompile(`\b` + recName + `\.`)
			body = re.ReplaceAllString(body, " ln.")
			body = body[strings.Index(body, name):]
			hash := toMd5(body)
			body = body[len(name)+1:]
			fullName := name + "_" + hash

			sig := body[:strings.Index(body, "{")]
			funcSig := "func(ln *localen"
			if !strings.HasPrefix(sig, ")") {
				funcSig += ", "
			}
			field := fmt.Sprintf("fn%s %s%s", name, funcSig, sig)
			method := fmt.Sprintf("fn%s fn%s", name, fullName)

			c.methods = append(c.methods, method)
			c.fields = append(c.fields, field)

			body = "var fn" + fullName + " = " + funcSig + body + "\n"

			if !c.methodSet[hash] {
				fmt.Fprint(c.w, body)
			}
			c.methodSet[hash] = true
		}
	case *ast.GenDecl:
		for _, spec := range vv.Specs {
			switch spec.(type) {
			case *ast.TypeSpec:
				typeSpec := spec.(*ast.TypeSpec)
				switch typeSpec.Type.(type) {
				case *ast.StructType:
					structType := typeSpec.Type.(*ast.StructType)
					for _, field := range structType.Fields.List {
						typeExpr := field.Type

						start := typeExpr.Pos() - 1
						end := typeExpr.End() - 1

						typeInSource := c.src[start:end]

						c.fields = append(c.fields, fmt.Sprintf("%s %s", field.Names[0].Name, string(typeInSource)))
					}
				}
			}
		}

	}

	return false
}

var returnStructRe = regexp.MustCompile(`return &.*{`)

func (c *coll) collectNew(node ast.Node) bool {
	switch vv := node.(type) {
	case *ast.FuncDecl:
		if vv.Name.Name == "New" {
			start := vv.Body.Pos() - 1
			end := vv.Body.End() - 1

			body := string(c.src[start : end-1])

			body = strings.Replace(body, "{\n\t", "", 1)
			body = strings.Replace(body, "\t}", "", 1)

			body = returnStructRe.ReplaceAllString(body, "")

			for _, method := range c.methods {
				if strings.HasPrefix(method, "fn") {
					parts := strings.Fields(method)
					body += fmt.Sprintf("\n%s: %s,", parts[0], parts[1])
				}
			}
			fmt.Fprintf(c.initW, "\ttranslatorFuncs[%q] = %s\n", strings.ToLower(c.locale), fmt.Sprintf("func() locales.Translator {\nreturn &localen{\n%s\n}\n}", body))
		}
	}
	return false
}

func uniqueStringsSorted(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	ss := sort.StringSlice(s)
	ss.Sort()
	i := 0
	for j := 1; j < len(s); j++ {
		if !ss.Less(i, j) {
			continue
		}
		i++
		s[i] = s[j]
	}

	return s[:i+1]
}

func toMd5(f string) string {
	h := md5.New()
	h.Write([]byte(f))
	return hex.EncodeToString(h.Sum([]byte{}))
}

func createCurrenciesMap() {
	cfg := &packages.Config{
		Mode:  packages.LoadSyntax,
		Tests: false,
	}

	pkgs, err := packages.Load(cfg, "github.com/gohugoio/locales/currency")
	if err != nil {
		log.Fatal(err)
	}

	pkg := pkgs[0]

	collector := &currencyCollector{}

	for _, f := range pkg.Syntax {
		ast.Inspect(f, collector.handleNode)
	}

	sort.Strings(collector.constants)

	f, err := os.Create(filepath.Join("../currencies.autogen.go"))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	fmt.Fprintf(f, "%s\nimport \"github.com/gohugoio/locales/currency\"\n", header)

	fmt.Fprintf(f, "var currencies = map[string]currency.Type {")
	for _, currency := range collector.constants {
		fmt.Fprintf(f, "\n%q: currency.%s,", currency, currency)
	}
	fmt.Fprintln(f, "}")
}

type currencyCollector struct {
	constants []string
}

func (c *currencyCollector) handleNode(node ast.Node) bool {
	decl, ok := node.(*ast.GenDecl)
	if !ok || decl.Tok != token.CONST {
		return true
	}
	typ := ""
	for _, spec := range decl.Specs {
		vspec := spec.(*ast.ValueSpec)
		if vspec.Type == nil && len(vspec.Values) > 0 {
			typ = ""

			ce, ok := vspec.Values[0].(*ast.CallExpr)
			if !ok {
				continue
			}
			id, ok := ce.Fun.(*ast.Ident)
			if !ok {
				continue
			}
			typ = id.Name
		}
		if vspec.Type != nil {
			ident, ok := vspec.Type.(*ast.Ident)
			if !ok {
				continue
			}
			typ = ident.Name
		}
		if typ != "Type" {
			// This is not the type we're looking for.
			continue
		}
		// We now have a list of names (from one line of source code) all being
		// declared with the desired type.
		// Grab their names and actual values and store them in f.values.
		for _, name := range vspec.Names {
			c.constants = append(c.constants, name.String())
		}
	}
	return false
}

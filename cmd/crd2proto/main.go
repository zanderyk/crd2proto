package main

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
	gtp "github.com/zanderyk/code-generator/cmd/go-to-protobuf/protobuf"
	"k8s.io/klog/v2"
)

var (
	messageStartRE = regexp.MustCompile(`^message\s+([A-Za-z_][A-Za-z0-9_]*)\s*\{`)

	// optional|required|repeated <type> <name> = <num>;
	// like ".k8s.io.apimachinery.pkg.apis.meta.v1.ObjectMeta" or local "Foo".
	fieldRE = regexp.MustCompile(`^(optional|required|repeated)\s+([A-Za-z0-9_.]+)\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*(\d+)\s*;`)

	cannotConvertRE = regexp.MustCompile(`type (\S+) cannot be converted to protobuf`)

	// discoverExternalPackagesCmd emits "<dir>\t<importpath>" for each dependency
	// package in the module graph.
	discoverExternalPackagesCmd = []string{
		"list", "-e", "-f",
		`{{if .Module}}{{if not .Module.Main}}{{.Dir}}` + "\t" + `{{.ImportPath}}{{end}}{{end}}`,
		"all",
	}

	gogoImport     = regexp.MustCompile(`(?m)^import\s+"github\.com/gogo/protobuf/gogoproto/gogo\.proto";\s*\n`)
	gogoOption     = regexp.MustCompile(`(?m)^option \(gogoproto\.[^;]+;\s*\n`)
	gogoFieldAnnot = regexp.MustCompile(`\s*\[\s*\(gogoproto\.[^\]]+\]`)

	goPackageRE = regexp.MustCompile(`option\s+go_package\s*=\s*"([^"]+)"`)
)

func main() {
	klog.LogToStderr(false)
	klog.SetOutput(io.Discard)
	log.SetOutput(io.Discard)

	root := &cobra.Command{
		Use:   "crd2proto",
		Short: "Generate .proto files from kubebuilder-style CRD Go types",
	}
	root.AddCommand(newGenerateCmd())
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// resolveExternalPackages returns the discovered packages
// (or the fallback) plus any additive override entries.
func resolveExternalPackages(ctx context.Context, override string) []string {
	pkgs, err := discoverExternalPackages(ctx, ".")
	if err != nil || len(pkgs) == 0 {
		if err != nil {
			fmt.Fprintf(os.Stderr, "warning: external-package discovery failed (%v); using built-in defaults\n", err)
		}
		pkgs = strings.Split(fallbackExternalPackages, ",")
	}
	seen := make(map[string]bool, len(pkgs))
	var out []string
	for _, p := range append(pkgs, strings.Split(override, ",")...) {
		if p = strings.TrimSpace(p); p != "" && !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// discoverExternalPackages walks the target repo's dependency graph and returns
// the resolve-only entries ("-"+importpath, go-to-protobuf syntax) for dep
// packages that ship a generated.proto.
func discoverExternalPackages(ctx context.Context, dir string) ([]string, error) {
	cmd := exec.CommandContext(ctx, "go", discoverExternalPackagesCmd...)
	cmd.Dir = dir

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("go list: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	var pkgs []string
	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		pkgDir, importPath, ok := strings.Cut(sc.Text(), "\t")
		if !ok || pkgDir == "" {
			continue // main-module and std-lib packages emit an empty line
		}
		if _, err := os.Stat(filepath.Join(pkgDir, "generated.proto")); err != nil {
			continue
		}
		pkgs = append(pkgs, "-"+importPath)
	}
	return pkgs, sc.Err()
}

func newGenerateCmd() *cobra.Command {
	g := gtp.New()
	g.OnlyIDL = true // skip protoc/gogo entirely as we handle tag injection ourselves

	var externalPkgs string

	cmd := &cobra.Command{
		Use:   "generate <go-package>",
		Short: "Inject protobuf struct tags into a CRD package and emit its generated.proto",
		Long: `Walks a Go package containing kubebuilder-style CRD types,
adds protobuf:"wire,N,label,name=..." struct tags to fields that don't have
them yet (writing modified .go files back to disk), then writes a
generated.proto next to the .go files.

Wraps k8s.io/code-generator/cmd/go-to-protobuf with no protoc/gogo
dependency and a hard-coded "external" package list (apimachinery +
k8s.io/api/*) so users don't have to manage the proto-import dance.

Pass <go-package> as a Go import path. Example:
    crd2proto generate my.domain/guestbook/api/v1`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			g.Packages = args[0]
			targets := targetPackages(args[0])
			exclude := resolveExternalPackages(cmd.Context(), externalPkgs)
			excluded := make(map[string]bool, len(exclude))
			for _, e := range exclude {
				excluded[strings.TrimPrefix(e, "-")] = true
			}

			// Retry, excluding each unserializable package until it converges.
			var protoless []string
			for {
				g.APIMachineryPackages = strings.Join(exclude, ",")
				err := gtp.RunLogic(g)
				if err == nil {
					break
				}
				badPkgs := unconvertiblePackages(err.Error())
				if len(badPkgs) == 0 {
					return fmt.Errorf("go-to-protobuf: %w", err)
				}
				added := 0
				for _, pkg := range badPkgs {
					if targets[pkg] {
						return fmt.Errorf("a type in target package %s is not protobuf-serializable: %w", pkg, err)
					}
					if excluded[pkg] {
						continue
					}
					excluded[pkg] = true
					exclude = append(exclude, "-"+pkg)
					protoless = append(protoless, pkg)
					added++
					fmt.Fprintf(os.Stderr, "auto-excluding unserializable package: %s\n", pkg)
				}
				if added == 0 {
					return fmt.Errorf("go-to-protobuf: %w", err)
				}
			}

			outDir := g.OutputDir
			if outDir == "" {
				outDir = "."
			}

			if err := stripGogoExtensions(outDir); err != nil {
				return fmt.Errorf("strip gogo: %w", err)
			}
			if err := stripMissingImports(outDir, protoless); err != nil {
				return fmt.Errorf("strip missing imports: %w", err)
			}

			for pkg := range strings.SplitSeq(args[0], ",") {
				pkg = strings.TrimSpace(pkg)
				// A leading "-" marks an external package (imported, not
				// generated), so there's no generated.proto of ours to inject.
				if pkg == "" || strings.HasPrefix(pkg, "-") {
					continue
				}
				if err := injectTagsForPackage(outDir, pkg); err != nil {
					return err
				}
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&g.OutputDir, "output-dir", g.OutputDir, "base directory under which to write generated.proto files (default: current dir)")
	cmd.Flags().StringVar(&g.GoHeaderFile, "go-header-file", g.GoHeaderFile, "file containing a boilerplate header to prepend to each generated.proto")
	cmd.Flags().StringVar(&externalPkgs, "external-packages", "", "extra resolve-only packages (\"-\"+importpath), added to the auto-discovered set; for deps whose generated.proto isn't in the module cache")
	cmd.Flags().StringVar(&g.DropEmbeddedFields, "drop-embedded-fields", g.DropEmbeddedFields, "comma-delimited Go types to omit from generated protobufs")
	return cmd
}

// unconvertiblePackages returns the deduped import paths of packages whose types
// go-to-protobuf reported as non-serializable in its error message.
func unconvertiblePackages(msg string) []string {
	var pkgs []string
	seen := make(map[string]bool)
	for _, m := range cannotConvertRE.FindAllStringSubmatch(msg, -1) {
		pkg := packageOfType(m[1])
		if pkg != "" && !seen[pkg] {
			seen[pkg] = true
			pkgs = append(pkgs, pkg)
		}
	}
	return pkgs
}

// packageOfType returns the import path of a fully-qualified type, e.g.
// "sigs.k8s.io/controller-runtime/pkg/scheme.Builder" -> ".../pkg/scheme".
func packageOfType(fq string) string {
	fq = strings.TrimLeft(fq, "*[]")
	slash := strings.LastIndex(fq, "/")
	seg := fq[slash+1:] // whole string when slash == -1
	before, _, ok := strings.Cut(seg, ".")
	if !ok {
		return ""
	}
	return fq[:slash+1] + before
}

// targetPackages returns the set of generated (non-resolve-only) import paths
// from a possibly comma-separated package argument.
func targetPackages(arg string) map[string]bool {
	targets := make(map[string]bool)
	for _, p := range strings.Split(arg, ",") {
		p = strings.TrimSpace(p)
		if p != "" && !strings.HasPrefix(p, "-") {
			targets[p] = true
		}
	}
	return targets
}

// injectTagsForPackage parses the package's generated.proto and splices the
// equivalent `protobuf:"..."` struct tags back into its .go source files
func injectTagsForPackage(outDir, pkgImportPath string) error {
	protoPath, err := findGeneratedProto(outDir, pkgImportPath)
	if err != nil {
		return err
	}
	messages, err := parseProtoMessages(protoPath)
	if err != nil {
		return fmt.Errorf("parse %s: %w", protoPath, err)
	}
	if len(messages) == 0 {
		return nil // nothing to inject
	}

	// go-to-protobuf assumes the caller is in the module root.
	srcDir := filepath.Dir(protoPath)
	goFiles, err := filepath.Glob(filepath.Join(srcDir, "*.go"))
	if err != nil {
		return err
	}

	protoToGo, err := mapProtoFieldsToGo(goFiles, messages)
	if err != nil {
		return fmt.Errorf("map proto fields: %w", err)
	}

	// build the structTags map the library's rewriter expects.
	structTags := buildStructTags(messages, protoToGo)
	for _, f := range goFiles {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		if err := gtp.RewriteTypesWithProtobufStructTags(f, structTags); err != nil {
			return fmt.Errorf("rewrite tags in %s: %w", f, err)
		}
	}
	return nil
}

// findGeneratedProto walks outDir for the generated.proto whose
// `option go_package = "..."` matches pkgImportPath.
func findGeneratedProto(outDir, pkgImportPath string) (string, error) {
	var found string
	err := filepath.WalkDir(outDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if filepath.Base(path) != "generated.proto" {
			return nil
		}
		gp, _ := readGoPackageOption(path)
		if gp == pkgImportPath {
			found = path
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("no generated.proto with option go_package=%q under %s", pkgImportPath, outDir)
	}
	return found, nil
}

func readGoPackageOption(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	m := goPackageRE.FindSubmatch(data)
	if m == nil {
		return "", nil
	}
	return string(m[1]), nil
}

// protoMessage is one `message X { ... }` block extracted from a .proto.
type protoMessage struct {
	Name   string
	Fields []protoField
}

type protoField struct {
	ProtoName string // proto field name (post `name =`)
	Number    int
	Label     string // "opt" / "req" / "rep"
	WireType  string // "bytes" / "varint" / "fixed32" / "fixed64"
}

// parseProtoMessages does a minimal regex-based extraction of message blocks
// and their `optional|required|repeated <type> <name> = <num>;` fields.
func parseProtoMessages(path string) ([]*protoMessage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []*protoMessage
	var cur *protoMessage
	depth := 0
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 1<<20), 8<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if cur == nil {
			if m := messageStartRE.FindStringSubmatch(line); m != nil {
				cur = &protoMessage{Name: m[1]}
				depth = 1
				continue
			}
			continue
		}
		if strings.Contains(line, "{") {
			depth++
		}
		if strings.Contains(line, "}") {
			depth--
			if depth == 0 {
				out = append(out, cur)
				cur = nil
				continue
			}
		}
		if m := fieldRE.FindStringSubmatch(line); m != nil {
			num, _ := strconv.Atoi(m[4])
			cur.Fields = append(cur.Fields, protoField{
				ProtoName: m[3],
				Number:    num,
				Label:     labelToTag(m[1]),
				WireType:  wireForProtoType(m[2]),
			})
		}
	}
	return out, scanner.Err()
}

func labelToTag(s string) string {
	switch s {
	case "optional":
		return "opt"
	case "required":
		return "req"
	case "repeated":
		return "rep"
	}
	return "opt"
}

// wireForProtoType returns the wire-type string the protobuf struct tag uses
// for a given .proto type token. Anything that isn't a primitive scalar is a
// message ref => "bytes".
func wireForProtoType(t string) string {
	switch t {
	case "bool", "int32", "int64", "uint32", "uint64", "sint32", "sint64",
		"enum":
		return "varint"
	case "float", "fixed32", "sfixed32":
		return "fixed32"
	case "double", "fixed64", "sfixed64":
		return "fixed64"
	case "string", "bytes":
		return "bytes"
	}
	return "bytes"
}

// mapProtoFieldsToGo builds, per message type, a map of proto field name -> Go
// field name. A proto name matches the json tag's name, or (no json tag) the
// lowerCamel of the Go field name.
func mapProtoFieldsToGo(goFiles []string, messages []*protoMessage) (map[string]map[string]string, error) {
	wantTypes := map[string]bool{}
	for _, m := range messages {
		wantTypes[m.Name] = true
	}
	out := map[string]map[string]string{} // type -> proto field -> go field
	fset := token.NewFileSet()
	for _, path := range goFiles {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			ts, ok := n.(*ast.TypeSpec)
			if !ok {
				return true
			}
			if !wantTypes[ts.Name.Name] {
				return true
			}
			st, ok := ts.Type.(*ast.StructType)
			if !ok {
				return true
			}
			for _, field := range st.Fields.List {
				goName := goFieldName(field)
				if goName == "" {
					continue
				}
				protoName := protoFieldName(field, goName)
				if protoName == "" {
					continue
				}
				if out[ts.Name.Name] == nil {
					out[ts.Name.Name] = map[string]string{}
				}
				out[ts.Name.Name][protoName] = goName
			}
			return true
		})
	}
	return out, nil
}

func goFieldName(f *ast.Field) string {
	if len(f.Names) > 0 {
		return f.Names[0].Name
	}
	t := f.Type
	if star, ok := t.(*ast.StarExpr); ok {
		t = star.X
	}
	switch x := t.(type) {
	case *ast.Ident:
		return x.Name
	case *ast.SelectorExpr:
		return x.Sel.Name
	}
	return ""
}

func protoFieldName(f *ast.Field, goName string) string {
	if jt := tagValue(f.Tag, "json"); jt != "" {
		if comma := strings.Index(jt, ","); comma >= 0 {
			jt = jt[:comma]
		}
		jt = strings.TrimSpace(jt)
		if jt != "" && jt != "-" {
			return jt
		}
		// json:",inline" — embedded struct, no proto field of its own
		// unless the user gave it one. Fall through to lowerCamel.
	}
	if goName == "" {
		return ""
	}
	return strings.ToLower(goName[:1]) + goName[1:]
}

func tagValue(t *ast.BasicLit, name string) string {
	if t == nil {
		return ""
	}
	unquoted, err := strconv.Unquote(t.Value)
	if err != nil {
		return ""
	}
	return reflect.StructTag(unquoted).Get(name)
}

// buildStructTags produces the map[type]map[goField]tag shape the rewriter
// consumes, each value a `protobuf:"<wire>,<num>,<label>,name=<proto>"` literal.
func buildStructTags(messages []*protoMessage, protoToGo map[string]map[string]string) map[string]map[string]string {
	out := map[string]map[string]string{}
	for _, msg := range messages {
		mapForType := protoToGo[msg.Name]
		if mapForType == nil {
			continue
		}
		dst := map[string]string{}
		for _, f := range msg.Fields {
			goName, ok := mapForType[f.ProtoName]
			if !ok {
				continue
			}
			dst[goName] = fmt.Sprintf(`protobuf:"%s,%d,%s,name=%s"`, f.WireType, f.Number, f.Label, f.ProtoName)
		}
		if len(dst) > 0 {
			out[msg.Name] = dst
		}
	}
	return out
}

func stripGogoExtensions(root string) error {
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if filepath.Base(path) != "generated.proto" {
			return nil
		}
		return rewriteFileWithoutGogo(path)
	})
}

// stripMissingImports drops `import "<pkg>/generated.proto";` lines for pkgs
// (import paths, no "-" prefix) that are resolve-only but ship no generated.proto.
func stripMissingImports(root string, pkgs []string) error {
	if len(pkgs) == 0 {
		return nil
	}
	imports := make([]string, len(pkgs))
	for i, p := range pkgs {
		imports[i] = fmt.Sprintf("%s/generated.proto", p)
	}
	return filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || filepath.Base(path) != "generated.proto" {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lines := strings.Split(string(data), "\n")
		kept := lines[:0]
		for _, line := range lines {
			if isImportOf(line, imports) {
				continue
			}
			kept = append(kept, line)
		}
		return os.WriteFile(path, []byte(collapseBlankLines(strings.Join(kept, "\n"))), 0600)
	})
}

func isImportOf(line string, protoPaths []string) bool {
	trimmedLine := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmedLine, "import ") {
		return false
	}
	for _, p := range protoPaths {
		if strings.Contains(trimmedLine, `"`+p+`"`) {
			return true
		}
	}
	return false
}

func rewriteFileWithoutGogo(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	out := string(data)
	out = gogoImport.ReplaceAllString(out, "")
	out = gogoOption.ReplaceAllString(out, "")
	out = gogoFieldAnnot.ReplaceAllString(out, "")
	out = collapseBlankLines(out)
	return os.WriteFile(path, []byte(out), 0600)
}

func collapseBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	var trimmed []string
	prevBlank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			if prevBlank {
				continue
			}
			prevBlank = true
		} else {
			prevBlank = false
		}
		trimmed = append(trimmed, line)
	}
	return strings.Join(trimmed, "\n")
}

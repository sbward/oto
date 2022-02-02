package parser

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/doc"
	"go/token"
	"go/types"
	"regexp"
	"sort"
	"strings"

	"github.com/fatih/structtag"
	"github.com/pkg/errors"
	"golang.org/x/tools/go/packages"
)

// ErrNotFound is returned when an Object is not found.
var ErrNotFound = errors.New("not found")

// Definition describes an Oto definition.
type Definition struct {
	// PackageName is the name of the package.
	PackageName string `json:"packageName"`
	// Services are the services described in this definition.
	Services []Service `json:"services"`
	// Objects are the structures that are used throughout this definition.
	Objects []Object `json:"objects"`
	// Imports is a map of Go imports that should be imported into
	// Go code.
	Imports map[string]string `json:"imports"`
}

// Object looks up an object by name. Returns ErrNotFound error
// if it cannot find it.
func (d *Definition) Object(name string) (*Object, error) {
	for i := range d.Objects {
		obj := &d.Objects[i]
		if obj.Name == name {
			return obj, nil
		}
	}
	return nil, ErrNotFound
}

// ObjectIsInput gets whether this object is a method
// input (request) type or not.\
// Returns true if any method.InputObject.ObjectName matches
// name.
func (d *Definition) ObjectIsInput(name string) bool {
	for _, service := range d.Services {
		for _, method := range service.Methods {
			if method.InputObject.ObjectName == name {
				return true
			}
		}
	}
	return false
}

// ObjectIsOutput gets whether this object is a method
// output (response) type or not.
// Returns true if any method.OutputObject.ObjectName matches
// name.
func (d *Definition) ObjectIsOutput(name string) bool {
	for _, service := range d.Services {
		for _, method := range service.Methods {
			if method.OutputObject.ObjectName == name {
				return true
			}
		}
	}
	return false
}

// Service describes a service, akin to an interface in Go.
type Service struct {
	Name    string   `json:"name"`
	Methods []Method `json:"methods"`
	Comment string   `json:"comment"`
	// Metadata are typed key/value pairs extracted from the
	// comments.
	Metadata map[string]interface{} `json:"metadata"`
}

// Method describes a method that a Service can perform.
type Method struct {
	Name           string `json:"name"`
	NameLowerCamel string `json:"nameLowerCamel"`
	InputObject    Type   `json:"inputObject"`
	OutputObject   Type   `json:"outputObject"`
	Comment        string `json:"comment"`
	// Metadata are typed key/value pairs extracted from the
	// comments.
	Metadata map[string]interface{} `json:"metadata"`
}

// Object describes a data structure that is part of this definition.
type Object struct {
	TypeID   string  `json:"typeID"`
	Name     string  `json:"name"`
	Imported bool    `json:"imported"`
	Package  string  `json:"package"`
	Fields   []Field `json:"fields"`
	Comment  string  `json:"comment"`
	// Metadata are typed key/value pairs extracted from the
	// comments.
	Metadata map[string]interface{} `json:"metadata"`
}

// Field describes the field inside an Object.
type Field struct {
	Name           string              `json:"name"`
	NameLowerCamel string              `json:"nameLowerCamel"`
	NameJSON       string              `json:"nameJSON"`
	Type           Type                `json:"type"`
	OmitEmpty      bool                `json:"omitEmpty"`
	Comment        string              `json:"comment"`
	Tag            string              `json:"tag"`
	ParsedTags     map[string]FieldTag `json:"parsedTags"`
	Example        interface{}         `json:"example"`
	// Metadata are typed key/value pairs extracted from the
	// comments.
	Metadata map[string]interface{} `json:"metadata"`
	Skip     bool
}

// FieldTag is a parsed tag.
// For more information, see Struct Tags in Go.
type FieldTag struct {
	// Value is the value of the tag.
	Value string `json:"value"`
	// Options are the options for the tag.
	Options []string `json:"options"`
}

// Type holds information about a data type.
type Type struct {
	TypeID     string `json:"typeID"`
	TypeName   string `json:"typeName"`
	ObjectName string `json:"objectName"`
	// CleanObjectName is the ObjectName with * removed
	// for pointer types.
	CleanObjectName      string `json:"cleanObjectName"`
	UnderlyingTypeName   string `json:"underlyingTypeName"`
	ObjectNameLowerCamel string `json:"objectNameLowerCamel"`
	Multiple             bool   `json:"multiple"`
	Package              string `json:"package"`
	IsObject             bool   `json:"isObject"`
	IsMap                bool   `json:"isMap"`
	// MapKeyType           Type   `json:"-"`
	// MapElemType          Type   `json:"-"`
	JSType    string `json:"jsType"`
	TSType    string `json:"tsType"`
	SwiftType string `json:"swiftType"`
}

// IsOptional returns true for pointer types (optional).
func (f Type) IsOptional() bool {
	return strings.HasPrefix(f.ObjectName, "*")
}

// Parser parses Oto Go definition packages.
type Parser struct {
	Verbose bool

	ExcludeInterfaces []string
	IncludeInterfaces []string

	patterns []string
	def      Definition

	// outputObjects marks output object names.
	outputObjects map[string]struct{}
	// objects marks object names.
	objects map[string]struct{}

	// docs are the docs for extracting comments.
	docs *doc.Package
}

// New makes a fresh parser using the specified patterns.
// The patterns should be the args passed into the tool (after any flags)
// and will be passed to the underlying build system.
func New(patterns ...string) *Parser {
	return &Parser{
		patterns: patterns,
	}
}

// Parse parses the files specified, returning the definition.
func (p *Parser) Parse() (Definition, error) {
	cfg := &packages.Config{
		Mode:  packages.NeedTypes | packages.NeedName | packages.NeedTypesInfo | packages.NeedDeps | packages.NeedName | packages.NeedSyntax,
		Tests: false,
	}
	pkgs, err := packages.Load(cfg, p.patterns...)
	if err != nil {
		return p.def, err
	}
	p.outputObjects = make(map[string]struct{})
	p.objects = make(map[string]struct{})
	var excludedObjectsTypeIDs []string
	for _, pkg := range pkgs {
		p.docs, err = doc.NewFromFiles(pkg.Fset, pkg.Syntax, "")
		if err != nil {
			panic(err)
		}
		p.def.PackageName = pkg.Name
		scope := pkg.Types.Scope()
		for _, name := range scope.Names() {
			obj := scope.Lookup(name)
			switch item := obj.Type().Underlying().(type) {
			case *types.Interface:
				if len(p.IncludeInterfaces) > 0 && !isInSlice(p.IncludeInterfaces, name) {
					continue
				}
				s, err := p.parseService(pkg, obj, item)
				if err != nil {
					return p.def, err
				}
				if isInSlice(p.ExcludeInterfaces, name) {
					for _, method := range s.Methods {
						excludedObjectsTypeIDs = append(excludedObjectsTypeIDs, method.InputObject.TypeID)
						excludedObjectsTypeIDs = append(excludedObjectsTypeIDs, method.OutputObject.TypeID)
					}
					continue
				}
				p.def.Services = append(p.def.Services, s)
			}
		}
	}
	// remove any excluded objects
	nonExcludedObjects := make([]Object, 0, len(p.def.Objects))
	for _, object := range p.def.Objects {
		excluded := false
		for _, excludedTypeID := range excludedObjectsTypeIDs {
			if object.TypeID == excludedTypeID {
				excluded = true
				break
			}
		}
		if excluded {
			continue
		}
		nonExcludedObjects = append(nonExcludedObjects, object)
	}
	p.def.Objects = nonExcludedObjects
	// sort services
	sort.Slice(p.def.Services, func(i, j int) bool {
		return p.def.Services[i].Name < p.def.Services[j].Name
	})
	// sort objects
	sort.Slice(p.def.Objects, func(i, j int) bool {
		return p.def.Objects[i].Name < p.def.Objects[j].Name
	})
	if err := p.addOutputFields(); err != nil {
		return p.def, err
	}
	return p.def, nil
}

func (p *Parser) parseService(pkg *packages.Package, obj types.Object, interfaceType *types.Interface) (Service, error) {
	var s Service
	s.Name = obj.Name()
	s.Comment = p.commentForType(s.Name)
	var err error
	s.Metadata, s.Comment, err = p.extractCommentMetadata(s.Comment)
	if err != nil {
		return s, p.wrapErr(errors.New("extract comment metadata"), pkg, obj.Pos())
	}
	if p.Verbose {
		fmt.Printf("%s ", s.Name)
	}
	l := interfaceType.NumMethods()
	for i := 0; i < l; i++ {
		m := interfaceType.Method(i)
		method, err := p.parseMethod(pkg, s.Name, m)
		if err != nil {
			return s, err
		}
		s.Methods = append(s.Methods, method)
	}
	return s, nil
}

func (p *Parser) parseMethod(pkg *packages.Package, serviceName string, methodType *types.Func) (Method, error) {
	var m Method
	m.Name = methodType.Name()
	if p.Verbose {
		fmt.Printf("%s ", m.Name)
	}
	m.NameLowerCamel = camelizeDown(m.Name)
	m.Comment = p.commentForMethod(serviceName, m.Name)
	var err error
	m.Metadata, m.Comment, err = p.extractCommentMetadata(m.Comment)
	if err != nil {
		return m, p.wrapErr(errors.New("extract comment metadata"), pkg, methodType.Pos())
	}
	sig := methodType.Type().(*types.Signature)
	inputParams := sig.Params()
	if inputParams.Len() == 2 && inputParams.At(0).Type().String() != "context.Context" {
		return m, p.wrapErr(errors.New("invalid method signature: expected first argument of two to be context.Context"), pkg, methodType.Pos())
	}
	if l := inputParams.Len(); l < 1 || l > 2 {
		return m, p.wrapErr(errors.New("invalid method signature: expected arguments (MethodRequest) or (context.Context, MethodRequest)"), pkg, methodType.Pos())
	}
	m.InputObject, err = p.parseTypeDecl(pkg, inputParams.At(inputParams.Len()-1))
	if err != nil {
		return m, errors.Wrap(err, "parse input object type")
	}
	outputParams := sig.Results()
	if outputParams.Len() == 2 && outputParams.At(1).Type().String() != "error" {
		return m, p.wrapErr(errors.New("invalid method signature: expected second return value of two to be error"), pkg, methodType.Pos())
	}
	if l := outputParams.Len(); l < 1 || l > 2 {
		return m, p.wrapErr(errors.New("invalid method signature: expected to return MethodResponse or (MethodResponse, error)"), pkg, methodType.Pos())
	}
	m.OutputObject, err = p.parseTypeDecl(pkg, outputParams.At(0))
	if err != nil {
		return m, errors.Wrap(err, "parse output object type")
	}
	p.outputObjects[m.OutputObject.TypeName] = struct{}{}
	return m, nil
}

// parseObject parses a struct type and adds it to the Definition.
func (p *Parser) parseObject(pkg *packages.Package, o types.Object, v *types.Struct) error {
	if _, ok := p.objects[o.Name()]; ok {
		return nil
	}
	var obj Object
	obj.Name = o.Name()
	if p.Verbose {
		fmt.Printf("%s ", obj.Name)
	}
	obj.Comment = p.commentForType(obj.Name)
	var err error
	obj.Metadata, obj.Comment, err = p.extractCommentMetadata(obj.Comment)
	if err != nil {
		return p.wrapErr(errors.New("extract comment metadata"), pkg, o.Pos())
	}
	if _, found := p.objects[obj.Name]; found {
		// if this has already been parsed, skip it
		return nil
	}
	if o.Pkg().Name() != pkg.Name {
		obj.Imported = true
		obj.Package = o.Pkg().Path()
	}
	typ := v.Underlying()
	st, ok := typ.(*types.Struct)
	if !ok {
		return p.wrapErr(errors.New(obj.Name+" must be a struct"), pkg, o.Pos())
	}
	obj.TypeID = o.Pkg().Path() + "." + obj.Name
	obj.Fields = []Field{}
	p.objects[obj.Name] = struct{}{}
	for i := 0; i < st.NumFields(); i++ {
		if !st.Field(i).Exported() {
			continue
		}
		field, err := p.parseField(pkg, obj.Name, st.Field(i), st.Tag(i))
		if err != nil {
			return err
		}
		if field.Skip {
			continue
		}
		obj.Fields = append(obj.Fields, field)
	}
	p.def.Objects = append(p.def.Objects, obj)
	return nil
}

func (p *Parser) parseTags(tag string) (map[string]FieldTag, error) {
	tags, err := structtag.Parse(tag)
	if err != nil {
		return nil, err
	}
	fieldTags := make(map[string]FieldTag)
	for _, tag := range tags.Tags() {
		fieldTags[tag.Key] = FieldTag{
			Value:   tag.Name,
			Options: tag.Options,
		}
	}
	return fieldTags, nil
}

func (p *Parser) parseField(pkg *packages.Package, objectName string, v *types.Var, tag string) (Field, error) {
	var f Field
	f.Name = v.Name()
	f.NameJSON = v.Name()
	if p.Verbose {
		fmt.Printf("%s ", f.Name)
	}
	f.NameLowerCamel = camelizeDown(f.Name)
	f.Tag = tag
	var err error
	f.ParsedTags, err = p.parseTags(tag)
	if err != nil {
		return f, errors.Wrap(err, "parse field tag")
	}
	// If it has a json tag, use that as the NameJSON.
	if jsonTag, ok := f.ParsedTags["json"]; ok {
		if jsonTag.Value == "-" {
			f.Skip = true
		}
		if jsonTag.Value != "" {
			f.NameLowerCamel = jsonTag.Value
			f.NameJSON = jsonTag.Value
		}
		for _, option := range jsonTag.Options {
			if option == "omitempty" {
				f.OmitEmpty = true
			}
		}
	}
	f.Comment = p.commentForField(objectName, f.Name)
	f.Metadata = map[string]interface{}{}
	if !v.Exported() {
		return f, p.wrapErr(errors.New(f.Name+" must be exported"), pkg, v.Pos())
	}
	f.Metadata, f.Comment, err = p.extractCommentMetadata(f.Comment)
	if err != nil {
		return f, p.wrapErr(errors.New("extract comment metadata"), pkg, v.Pos())
	}
	if example, ok := f.Metadata["example"]; ok {
		f.Example = example
	}
	if !f.Skip {
		f.Type, err = p.parseTypeDecl(pkg, v)
		if err != nil {
			return f, errors.Wrap(err, "parse type")
		}
	}
	return f, nil
}

func (p *Parser) parseTypeDecl(pkg *packages.Package, obj types.Object) (Type, error) {
	var t Type
	pkgPath := pkg.PkgPath
	resolver := func(other *types.Package) string {
		if other.Name() != pkg.Name {
			if p.def.Imports == nil {
				p.def.Imports = make(map[string]string)
			}
			p.def.Imports[other.Path()] = other.Name()
			t.Package = other.Path()
			pkgPath = other.Path()
			return other.Name()
		}
		return "" // no package prefix
	}

	typ := obj.Type()
	if p.Verbose {
		fmt.Printf("%s ", typ.String())
	}
	if slice, ok := obj.Type().(*types.Slice); ok {
		typ = slice.Elem()
		t.Multiple = true
	}
	isPointer := true
	originalTyp := typ
	pointerType, isPointer := typ.(*types.Pointer)
	if isPointer {
		typ = pointerType.Elem()
		isPointer = true
	}
	ut := typ.Underlying()
	if named, ok := typ.(*types.Named); ok && typ.String() != "time.Time" {
		if structure, ok := named.Underlying().(*types.Struct); ok {
			if err := p.parseObject(pkg, named.Obj(), structure); err != nil {
				return t, err
			}
			t.IsObject = true
		}
		ut = named.Underlying()
	}
	if mapType, ok := typ.(*types.Map); ok {
		t.IsMap = true
		if named, ok := mapType.Key().(*types.Named); ok {
			if structure, ok := named.Underlying().(*types.Struct); ok {
				if err := p.parseObject(pkg, named.Obj(), structure); err != nil {
					return t, err
				}
			}
		}
		if named, ok := mapType.Elem().(*types.Named); ok {
			if structure, ok := named.Underlying().(*types.Struct); ok {
				if err := p.parseObject(pkg, named.Obj(), structure); err != nil {
					return t, err
				}
			}
		}
	}
	if typ.String() == "time.Time" {
		t.Package = ""
		t.UnderlyingTypeName = "string" // time.Time marshals itself to string
	} else {
		t.UnderlyingTypeName = strings.TrimPrefix(types.TypeString(ut, func(other *types.Package) string { return "" }), "*")
	}

	// disallow nested structs
	switch typ.(type) {
	case *types.Struct:
		return t, p.wrapErr(errors.New("nested structs not supported (create another type instead)"), pkg, obj.Pos())
	}
	t.TypeName = types.TypeString(originalTyp, resolver)
	t.ObjectName = types.TypeString(originalTyp, func(other *types.Package) string { return "" })
	t.ObjectNameLowerCamel = camelizeDown(t.ObjectName)
	t.TypeID = pkgPath + "." + t.ObjectName
	t.CleanObjectName = strings.TrimPrefix(t.ObjectName, "*")
	t.TSType = t.CleanObjectName
	t.JSType = t.CleanObjectName
	t.SwiftType = t.CleanObjectName
	if t.IsObject {
		t.JSType = "object"
		//ftype.SwiftType = "Any"
	} else if t.IsMap {
		// TODO ftype.TSType = fmt.Sprintf("Map<%s,%s>", keyType, elemType)
		t.JSType = "object"
	} else {
		switch t.UnderlyingTypeName {
		case "interface{}":
			t.JSType = "any"
			t.SwiftType = "Any"
			t.TSType = "object"
		case "map[string]interface{}":
			t.JSType = "object"
			t.TSType = "object"
			t.SwiftType = "Any"
		case "string":
			t.JSType = "string"
			t.SwiftType = "String"
			t.TSType = "string"
		case "bool":
			t.JSType = "boolean"
			t.SwiftType = "Bool"
			t.TSType = "boolean"
		case "int", "int16", "int32", "int64",
			"uint", "uint16", "uint32", "uint64",
			"float32", "float64":
			t.JSType = "number"
			t.SwiftType = "Double"
			t.TSType = "number"
		}
	}

	return t, nil
}

// addOutputFields adds built-in fields to the response objects
// mentioned in p.outputObjects.
func (p *Parser) addOutputFields() error {
	errorField := Field{
		OmitEmpty:      true,
		Name:           "Error",
		NameLowerCamel: "error",
		Comment:        "Error is string explaining what went wrong. Empty if everything was fine.",
		Type: Type{
			TypeName:  "string",
			JSType:    "string",
			SwiftType: "String",
			TSType:    "string",
		},
		Metadata: map[string]interface{}{},
		Example:  "something went wrong",
	}
	for typeName := range p.outputObjects {
		obj, err := p.def.Object(typeName)
		if err != nil {
			// skip if we can't find it - it must be excluded
			continue
		}
		obj.Fields = append(obj.Fields, errorField)
	}
	return nil
}

func (p *Parser) wrapErr(err error, pkg *packages.Package, pos token.Pos) error {
	position := pkg.Fset.Position(pos)
	return errors.Wrap(err, position.String())
}

func isInSlice(slice []string, s string) bool {
	for i := range slice {
		if slice[i] == s {
			return true
		}
	}
	return false
}

func (p *Parser) lookupType(name string) *doc.Type {
	for i := range p.docs.Types {
		if p.docs.Types[i].Name == name {
			return p.docs.Types[i]
		}
	}
	return nil
}

func (p *Parser) commentForType(name string) string {
	typ := p.lookupType(name)
	if typ == nil {
		return ""
	}
	return cleanComment(typ.Doc)
}

func (p *Parser) commentForMethod(service, method string) string {
	typ := p.lookupType(service)
	if typ == nil {
		return ""
	}
	spec, ok := typ.Decl.Specs[0].(*ast.TypeSpec)
	if !ok {
		return ""
	}
	iface, ok := spec.Type.(*ast.InterfaceType)
	if !ok {
		return ""
	}
	var m *ast.Field
outer:
	for i := range iface.Methods.List {
		for _, name := range iface.Methods.List[i].Names {
			if name.Name == method {
				m = iface.Methods.List[i]
				break outer
			}
		}
	}
	if m == nil {
		return ""
	}
	return cleanComment(m.Doc.Text())
}

func (p *Parser) commentForField(typeName, field string) string {
	typ := p.lookupType(typeName)
	if typ == nil {
		return ""
	}
	spec, ok := typ.Decl.Specs[0].(*ast.TypeSpec)
	if !ok {
		return ""
	}
	obj, ok := spec.Type.(*ast.StructType)
	if !ok {
		return ""
	}
	var f *ast.Field
outer:
	for i := range obj.Fields.List {
		for _, name := range obj.Fields.List[i].Names {
			if name.Name == field {
				f = obj.Fields.List[i]
				break outer
			}
		}
	}
	if f == nil {
		return ""
	}
	return cleanComment(f.Doc.Text())
}

func cleanComment(s string) string {
	return strings.TrimSpace(s)
}

// metadataCommentRegex is the regex to pull key value metadata
// used since we can't simply trust lines that contain a colon
var metadataCommentRegex = regexp.MustCompile(`^.*: .*`)

// extractCommentMetadata extracts key value pairs from the comment.
// It returns a map of metadata, and the
// remaining comment string.
// Metadata fields should succeed the comment string.
func (p *Parser) extractCommentMetadata(comment string) (map[string]interface{}, string, error) {
	var lines []string
	var metadata = make(map[string]interface{})
	s := bufio.NewScanner(strings.NewReader(comment))
	for s.Scan() {
		line := strings.TrimSpace(s.Text())
		if metadataCommentRegex.MatchString(line) {
			line = strings.TrimSpace(line)
			if line == "" {
				return metadata, strings.Join(lines, "\n"), nil
			}
			// SplitN is being used to ensure that colons can exist
			// in values by only splitting on the first colon in the line
			splitLine := strings.SplitN(line, ": ", 2)
			key := splitLine[0]
			value := strings.TrimSpace(splitLine[1])
			var val interface{}
			if err := json.Unmarshal([]byte(value), &val); err != nil {
				if p.Verbose {
					fmt.Printf("(skipping) failed to marshal JSON value (%s): %s\n", err, value)
				}
				continue
			}
			metadata[key] = val
			continue
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines = append(lines, line)
	}
	return metadata, strings.Join(lines, "\n"), nil
}

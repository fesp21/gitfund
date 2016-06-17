// Public Domain (-) 2016 The GitFund Authors.
// See the GitFund UNLICENSE file for details.

package main

import (
	"bytes"
	"fmt"
	"os"
	"reflect"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/tav/gitfund/app/model"
	"google.golang.org/appengine/datastore"
)

var (
	typeOfByteSlice  = reflect.TypeOf([]byte(nil))
	typeOfByteString = reflect.TypeOf(datastore.ByteString(nil))
	typeOfTime       = reflect.TypeOf(time.Time{})
)

type Prop struct {
	field    string
	multiple bool
	name     string
	noindex  bool
	typ      string
}

type Schema struct {
	dbfields map[string]*Prop
	fields   map[string]*Prop
	kind     string
	props    []*Prop
	slices   []string
}

func exit(format string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "ERROR: "+format+"\n", args...)
	os.Exit(1)
}

func main() {

	// Sort the entity kinds into alphabetical order.
	keys := []string{}
	for key, _ := range model.KindRegistry {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	// Define a map to hold the schema definitions.
	schemas := map[string]*Schema{}

	// Create the buffer to write to.
	buf := &bytes.Buffer{}

	// Spit out the header with the opening for the const block.
	buf.WriteString(`// AUTOGENERATED. DO NOT EDIT.
package model

import (
	"fmt"
	"google.golang.org/appengine/datastore"
	"time"
)

const (
`)

	// Loop over each entity kind, validate, and write the relevant metadata.
	for _, key := range keys {
		rt := reflect.TypeOf(model.KindRegistry[key])
		kind := rt.Name()
		fmt.Fprintf(buf, "\t%sKind = %q\n", kind, key)
		n := rt.NumField()
		seen := map[string]string{}
		dbfields := map[string]*Prop{}
		fields := map[string]*Prop{}
		props := []*Prop{}
		slices := []string{}
		for i := 0; i < n; i++ {
			field := rt.Field(i)
			// Skip unexported fields.
			r, _ := utf8.DecodeRuneInString(field.Name)
			if !unicode.IsUpper(r) {
				continue
			}
			// Ensure the field has a datastore struct tag.
			tag := field.Tag.Get("datastore")
			if tag == "" {
				exit("missing datastore struct tag for %s.%s", kind, field.Name)
			}
			// Ensure the field name hasn't been used already.
			split := strings.Split(tag, ",")
			name := ""
			noindex := false
			switch len(split) {
			case 2:
				name = split[0]
				if split[1] == "noindex" {
					noindex = true
				} else {
					exit("invalid struct tag for %s.%s: %q", kind, field.Name, tag)
				}
			case 1:
				name = split[0]
			default:
				exit("invalid struct tag for %s.%s: %q", kind, field.Name, tag)
			}
			name = strings.TrimSpace(name)
			if name == "" {
				exit("empty datastore field name for %s.%s: %q", kind, field.Name, tag)
			}
			if prev, exists := seen[name]; exists {
				exit("datastore field name %q for %s.%s already used for %s.%s", name, kind, field.Name, kind, prev)
			}
			seen[name] = field.Name
			// Define the property.
			ft := field.Type
			multiple := false
			typ := ""
			switch ft {
			case typeOfByteSlice:
				typ = "[]byte"
			case typeOfByteString:
				typ = "datastore.ByteString"
			case typeOfTime:
				typ = "time.Time"
			default:
				switch ft.Kind() {
				case reflect.Bool:
					typ = "bool"
				case reflect.Float64:
					typ = "float64"
				case reflect.Int64:
					typ = "int64"
				case reflect.Slice:
					multiple = true
					st := ft.Elem()
					switch ft.Elem() {
					case typeOfByteSlice:
						noindex = true
						typ = "[]byte"
					default:
						slices = append(slices, field.Name)
						switch st.Kind() {
						case reflect.String:
							typ = "string"
						default:
							exit("unsuported slice type %s specified for %s.%s", ft, kind, field.Name)
						}
					}
				case reflect.String:
					typ = "string"
				default:
					exit("unsupported type %s specified %s.%s", ft, kind, field.Name)
				}
			}
			prop := &Prop{
				field:    field.Name,
				multiple: multiple,
				name:     name,
				noindex:  noindex,
				typ:      typ,
			}
			props = append(props, prop)
			dbfields[name] = prop
			fields[field.Name] = prop
			// If not indexed, skip writing out the field name.
			if noindex {
				continue
			}
			fmt.Fprintf(buf, "\t%s_%s = \"%s =\"\n", kind, field.Name, name)
			fmt.Fprintf(buf, "\t%s_%s_asc = \"%s\"\n", kind, field.Name, name)
			fmt.Fprintf(buf, "\t%s_%s_desc = \"-%s\"\n", kind, field.Name, name)
			fmt.Fprintf(buf, "\t%s_%s_gt = \"%s >\"\n", kind, field.Name, name)
			fmt.Fprintf(buf, "\t%s_%s_gte = \"%s >=\"\n", kind, field.Name, name)
			fmt.Fprintf(buf, "\t%s_%s_lt = \"%s <\"\n", kind, field.Name, name)
			fmt.Fprintf(buf, "\t%s_%s_lte = \"%s <=\"\n", kind, field.Name, name)
		}
		schemas[key] = &Schema{
			dbfields: dbfields,
			fields:   fields,
			kind:     kind,
			props:    props,
			slices:   slices,
		}
	}

	// Close the const block and write a dummy variable using the time package
	// in case time.Time isn't used anywhere.
	buf.WriteString(`)

var _ = time.Time{}
`)

	// Loop through the schema definitions and write out Load/Save methods.
	for _, key := range keys {
		schema := schemas[key]
		initial := strings.ToLower(string(schema.kind[0]))
		// Sort the model fields into alphabetical order.
		fields := []string{}
		for field, _ := range schema.fields {
			fields = append(fields, field)
		}
		sort.Strings(fields)
		// Write the header for the Load method.
		fmt.Fprintf(buf, `

func (%s *%s) Load(props []datastore.Property) error {
	ok := true
`, initial, schema.kind)
		sort.Strings(schema.slices)
		for _, field := range schema.slices {
			prop := schema.fields[field]
			fmt.Fprintf(buf, "\t%s.%s = []%s{}\n", initial, field, prop.typ)
		}
		fmt.Fprint(buf, "\tfor _, prop := range props {\n\t\tswitch prop.Name {\n")
		// Sort the datastore field names into alphabetical order.
		dbfields := []string{}
		for dbfield, _ := range schema.dbfields {
			dbfields = append(dbfields, dbfield)
		}
		sort.Strings(dbfields)
		// Loop through the fields and write out code to load the property.
		for _, dbfield := range dbfields {
			prop := schema.dbfields[dbfield]
			fmt.Fprintf(buf, "\t\tcase %q:\n", dbfield)
			if prop.multiple {
				fmt.Fprintf(buf, "\t\t\tval, ok := prop.Value.(%s)\n", prop.typ)
				fmt.Fprintf(buf, `			if !ok {
				return fmt.Errorf("model: property for %s.%s element is not %s")
			}
`, schema.kind, prop.field, prop.typ)
				fmt.Fprintf(
					buf, "\t\t\t%s.%s = append(%s.%s, val)\n",
					initial, prop.field, initial, prop.field)
			} else {
				fmt.Fprintf(buf, "\t\t\t%s.%s, ok = prop.Value.(%s)\n", initial, prop.field, prop.typ)
				fmt.Fprintf(buf, `			if !ok {
				return fmt.Errorf("model: property for %s.%s is not %s")
			}
`, schema.kind, prop.field, prop.typ)
			}
		}
		// Write the footer for the Load method.
		fmt.Fprint(buf, "\t\t}\n\t}\n\treturn nil\n}")
		// Write the header for the Save method.
		fmt.Fprintf(buf, `

func (%s *%s) Save() ([]datastore.Property, error) {
	props := []datastore.Property{}
`, initial, schema.kind)
		// Loop through the fields and write out code to save the property.
		for _, field := range fields {
			prop := schema.fields[field]
			if prop.multiple {
				fmt.Fprintf(buf, "\tfor _, elem := range %s.%s {\n", initial, field)
				fmt.Fprint(buf, "\t\tprops = append(props, datastore.Property{\n")
				fmt.Fprintf(buf, "\t\t\tName: %q,\n", prop.name)
				if prop.noindex {
					fmt.Fprint(buf, "\t\t\tNoIndex: true,\n")
				}
				fmt.Fprint(buf, "\t\t\tMultiple: true,\n")
				fmt.Fprint(buf, "\t\t\tValue: elem,\n")
				fmt.Fprint(buf, "\t\t})\n")
				fmt.Fprint(buf, "\t}\n")
			} else {
				fmt.Fprint(buf, "\tprops = append(props, datastore.Property{\n")
				fmt.Fprintf(buf, "\t\tName: %q,\n", prop.name)
				if prop.noindex {
					fmt.Fprint(buf, "\t\tNoIndex: true,\n")
				}
				fmt.Fprintf(buf, "\t\tValue: %s.%s,\n", initial, field)
				fmt.Fprint(buf, "\t})\n")
			}
		}
		// Write the footer for the Save method.
		fmt.Fprint(buf, "\treturn props, nil\n}\n")
	}

	// Dump the buffer to stdout.
	fmt.Print(buf.String())

}
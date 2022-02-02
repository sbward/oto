package parser

import (
	"testing"

	"github.com/matryer/is"
)

func TestObjectExample(t *testing.T) {
	is := is.New(t)

	obj1 := Object{
		Name: "obj1",
		Fields: []Field{
			{
				Name:           "Name",
				NameLowerCamel: "name",
				Example:        "Mat",
			},
			{
				Name:           "Project",
				NameLowerCamel: "project",
				Example:        "Respond",
			},
			{
				Name:           "SinceYear",
				NameLowerCamel: "sinceYear",
				Example:        2021,
			},
			{
				Name:           "Favourites",
				NameLowerCamel: "favourites",
				Type: Type{
					TypeName:        "obj2",
					IsObject:        true,
					CleanObjectName: "obj2",
				},
			},
		},
	}
	obj2 := Object{
		Name: "obj2",
		Fields: []Field{
			{
				Type:           Type{TypeName: "string", Multiple: true, CleanObjectName: "string"},
				NameLowerCamel: "languages",
				Example:        "Go",
			},
		},
	}
	def := &Definition{
		Objects: []Object{obj1, obj2},
	}
	example, err := def.Example(obj1)
	is.NoErr(err)
	is.True(example != nil)

	is.Equal(example["name"], "Mat")
	is.Equal(example["project"], "Respond")
	is.Equal(example["sinceYear"], 2021)
	is.True(example["favourites"] != nil)
	favourites, ok := example["favourites"].(map[string]interface{})
	is.True(ok) // Favourites map[string]interface{}
	languages, ok := favourites["languages"].([]interface{})
	is.True(ok) // Languages []interface{}
	is.Equal(len(languages), 3)

	exampleJSON, err := def.Example(obj1)
	is.NoErr(err)
	is.Equal(len(exampleJSON), 4)

}
